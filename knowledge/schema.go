package gowild_knowledge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	data "github.com/original-david-knight/go_wild/data"
)

// EmbeddingDimensions is the width of every stored embedding. Embedders must
// return vectors of exactly this length.
const EmbeddingDimensions = 768

// maxIndexedText bounds the text one search row carries; PostgreSQL refuses
// tsvectors over 1 MB, and nothing past this point helps ranking.
const maxIndexedText = 400_000

// EnsureSearchSchema creates the kb_search table and its indexes. It is
// idempotent and runs on every connect. On PostgreSQL it also enables
// pgvector when the server has it installed; without it the embedding
// column is absent and search runs keyword-only until the extension appears
// and the service reconnects.
func EnsureSearchSchema(db data.Database) error {
	ctx := context.Background()
	exec, backend, err := data.Raw(db)
	if err != nil {
		return err
	}
	var statements []string
	if backend == data.BackendPostgres {
		statements = []string{
			`CREATE TABLE IF NOT EXISTS kb_search (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL,
				source_id TEXT NOT NULL DEFAULT '',
				context TEXT NOT NULL DEFAULT '',
				occurred_at TIMESTAMPTZ NOT NULL,
				title TEXT NOT NULL DEFAULT '',
				body TEXT NOT NULL DEFAULT '',
				weight DOUBLE PRECISION NOT NULL DEFAULT 1,
				active BOOLEAN NOT NULL DEFAULT TRUE,
				text_hash TEXT NOT NULL DEFAULT '',
				embed_state TEXT NOT NULL DEFAULT 'pending',
				embed_error TEXT NOT NULL DEFAULT '',
				updated_at TIMESTAMPTZ NOT NULL,
				tsv tsvector GENERATED ALWAYS AS (
					setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
					setweight(to_tsvector('english', coalesce(body, '')), 'B')
				) STORED
			)`,
			`CREATE INDEX IF NOT EXISTS kb_search_tsv ON kb_search USING gin (tsv)`,
			`CREATE INDEX IF NOT EXISTS kb_search_occurred ON kb_search (occurred_at DESC)`,
			`CREATE INDEX IF NOT EXISTS kb_search_pending ON kb_search (embed_state) WHERE embed_state = 'pending'`,
			`CREATE INDEX IF NOT EXISTS kb_items_source ON kb_items (source_id)`,
			`CREATE INDEX IF NOT EXISTS kb_facts_created ON kb_facts (created_at DESC)`,
		}
	} else {
		statements = []string{
			`CREATE TABLE IF NOT EXISTS kb_search (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL,
				source_id TEXT NOT NULL DEFAULT '',
				context TEXT NOT NULL DEFAULT '',
				occurred_at TEXT NOT NULL,
				title TEXT NOT NULL DEFAULT '',
				body TEXT NOT NULL DEFAULT '',
				weight REAL NOT NULL DEFAULT 1,
				active INTEGER NOT NULL DEFAULT 1,
				text_hash TEXT NOT NULL DEFAULT '',
				embed_state TEXT NOT NULL DEFAULT 'pending',
				embed_error TEXT NOT NULL DEFAULT '',
				updated_at TEXT NOT NULL,
				embedding BLOB
			)`,
			`CREATE INDEX IF NOT EXISTS kb_search_occurred ON kb_search (occurred_at)`,
			`CREATE INDEX IF NOT EXISTS kb_items_source ON kb_items (source_id)`,
		}
	}
	for _, stmt := range statements {
		if _, err := exec.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("knowledge schema: %w", err)
		}
	}
	if backend != data.BackendPostgres {
		return nil
	}
	if _, err := exec.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		// The extension is optional: search degrades to keyword-only.
		slog.Warn("knowledge: pgvector unavailable, semantic search disabled", "err", err)
		return nil
	}
	for _, stmt := range []string{
		fmt.Sprintf(`ALTER TABLE kb_search ADD COLUMN IF NOT EXISTS embedding vector(%d)`, EmbeddingDimensions),
		`CREATE INDEX IF NOT EXISTS kb_search_embedding ON kb_search USING hnsw (embedding vector_cosine_ops)`,
	} {
		if _, err := exec.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("knowledge vector schema: %w", err)
		}
	}
	return nil
}

// vectorSupport reports whether db can store and rank embeddings, and on
// PostgreSQL whether pgvector supports iterative index scans (0.8+).
func vectorSupport(ctx context.Context, db data.Database) (ok bool, iterative bool, err error) {
	exec, backend, err := data.Raw(db)
	if err != nil {
		return false, false, err
	}
	if backend == data.BackendSqlite {
		return true, false, nil
	}
	var version string
	row := exec.QueryRowContext(ctx, `SELECT e.extversion FROM pg_extension e
		WHERE e.extname = 'vector' AND EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'kb_search' AND column_name = 'embedding')`)
	if err := row.Scan(&version); err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return false, false, nil
		}
		return false, false, err
	}
	return true, versionAtLeast(version, 0, 8), nil
}

func versionAtLeast(version string, major, minor int) bool {
	var ma, mi int
	if _, err := fmt.Sscanf(version, "%d.%d", &ma, &mi); err != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}

// rebind rewrites "?" placeholders to "$n" for PostgreSQL. Queries in this
// package never contain a literal question mark.
func rebind(backend data.Backend, query string) string {
	if backend != data.BackendPostgres {
		return query
	}
	var sb strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&sb, "$%d", n)
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
