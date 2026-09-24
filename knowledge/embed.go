package gowild_knowledge

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	data "github.com/original-david-knight/go_wild/data"
)

// Embedder turns text into vectors of EmbeddingDimensions floats. Documents
// and queries are embedded differently by retrieval models, so both are
// asked for explicitly.
type Embedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// EmbedPending embeds up to batch rows whose text changed since they were
// last embedded, distilled records first and then the newest items. It
// returns how many rows it embedded; zero with a nil error means nothing is
// waiting (or semantic search is unavailable). Run it on a timer.
func (s *Service) EmbedPending(ctx context.Context, db data.Database, batch int) (int, error) {
	if s.embedder == nil || !s.vectorCaps(ctx, db).ok {
		return 0, nil
	}
	if batch <= 0 || batch > 100 {
		batch = 50
	}
	exec, backend, err := data.Raw(db)
	if err != nil {
		return 0, err
	}
	active := "active"
	if backend == data.BackendSqlite {
		active = "active = 1"
	}
	rows, err := exec.QueryContext(ctx, rebind(backend, `SELECT id, title, body, text_hash FROM kb_search
		WHERE embed_state = 'pending' AND `+active+`
		ORDER BY CASE WHEN kind = 'item' THEN 1 ELSE 0 END, occurred_at DESC LIMIT ?`), batch)
	if err != nil {
		return 0, err
	}
	type pending struct{ id, text, hash string }
	var work []pending
	for rows.Next() {
		var p pending
		var title, body string
		if err := rows.Scan(&p.id, &title, &body, &p.hash); err != nil {
			rows.Close()
			return 0, err
		}
		p.text = truncateRunes(strings.TrimSpace(title+"\n\n"+body), embedCharLimit)
		work = append(work, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(work) == 0 {
		return 0, nil
	}
	texts := make([]string, len(work))
	for i, p := range work {
		texts[i] = p.text
		if texts[i] == "" {
			texts[i] = "(empty)"
		}
	}
	vecs, err := s.embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("embed %d rows: %w", len(work), err)
	}
	if len(vecs) != len(work) {
		return 0, fmt.Errorf("embedder returned %d vectors for %d texts", len(vecs), len(work))
	}
	done := 0
	for i, p := range work {
		vec := vecs[i]
		if len(vec) != EmbeddingDimensions {
			if _, err := exec.ExecContext(ctx, rebind(backend, `UPDATE kb_search SET embed_state = 'failed', embed_error = ? WHERE id = ? AND text_hash = ?`),
				fmt.Sprintf("embedder returned %d dimensions, want %d", len(vec), EmbeddingDimensions), p.id, p.hash); err != nil {
				return done, err
			}
			continue
		}
		normalize(vec)
		var arg any
		if backend == data.BackendPostgres {
			arg = vectorLiteral(vec)
		} else {
			arg = encodeVector(vec)
		}
		cast := "?"
		if backend == data.BackendPostgres {
			cast = "?::vector"
		}
		// The text_hash guard skips rows edited while this batch was out.
		if _, err := exec.ExecContext(ctx, rebind(backend, `UPDATE kb_search SET embedding = `+cast+`, embed_state = 'done', embed_error = ''
			WHERE id = ? AND text_hash = ?`), arg, p.id, p.hash); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// Status summarizes the knowledge base.
type Status struct {
	Items            int  `json:"items"`
	Facts            int  `json:"facts"`
	Notes            int  `json:"notes"`
	Entities         int  `json:"entities"`
	Sources          int  `json:"sources"`
	PendingEmbedding int  `json:"pending_embedding"`
	FailedEmbedding  int  `json:"failed_embedding"`
	Semantic         bool `json:"semantic"`
}

// GetStatus counts records and embedding progress.
func (s *Service) GetStatus(ctx context.Context, db data.Database) (*Status, error) {
	exec, backend, err := data.Raw(db)
	if err != nil {
		return nil, err
	}
	active := "active"
	if backend == data.BackendSqlite {
		active = "active = 1"
	}
	st := &Status{Semantic: s.embedder != nil && s.vectorCaps(ctx, db).ok}
	rows, err := exec.QueryContext(ctx, `SELECT kind, count(*) FROM kb_search WHERE `+active+` GROUP BY kind`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			rows.Close()
			return nil, err
		}
		switch kind {
		case KindItem:
			st.Items = n
		case KindFact:
			st.Facts = n
		case KindNote:
			st.Notes = n
		case KindEntity:
			st.Entities = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := exec.QueryRowContext(ctx, `SELECT
			coalesce(sum(CASE WHEN embed_state = 'pending' AND `+active+` THEN 1 ELSE 0 END), 0),
			coalesce(sum(CASE WHEN embed_state = 'failed' THEN 1 ELSE 0 END), 0)
		FROM kb_search`).Scan(&st.PendingEmbedding, &st.FailedEmbedding); err != nil {
		return nil, err
	}
	if err := exec.QueryRowContext(ctx, `SELECT count(*) FROM kb_sources`).Scan(&st.Sources); err != nil {
		return nil, err
	}
	return st, nil
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	n := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= n
	}
}

func dot(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

func vectorLiteral(v []float32) string {
	var sb strings.Builder
	sb.Grow(len(v) * 10)
	sb.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

func encodeVector(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func decodeVector(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}
