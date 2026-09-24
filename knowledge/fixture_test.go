package gowild_knowledge

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

// pgDSN is a throwaway PostgreSQL cluster started once for the package, or
// "" when initdb is not installed (the PostgreSQL cases then skip).
var pgDSN string

func TestMain(m *testing.M) {
	stop := startPostgres()
	code := m.Run()
	stop()
	os.Exit(code)
}

func startPostgres() func() {
	if os.Getenv("KB_TEST_NO_POSTGRES") != "" {
		return func() {}
	}
	bindir := ""
	if out, err := exec.Command("pg_config", "--bindir").Output(); err == nil {
		bindir = strings.TrimSpace(string(out))
	}
	initdb := filepath.Join(bindir, "initdb")
	pgctl := filepath.Join(bindir, "pg_ctl")
	if _, err := os.Stat(initdb); err != nil {
		return func() {}
	}
	dir, err := os.MkdirTemp("", "kbtest")
	if err != nil {
		return func() {}
	}
	cleanup := func() { os.RemoveAll(dir) }
	if out, err := exec.Command(initdb, "-D", dir+"/data", "--auth=trust", "--no-locale", "--encoding=UTF8", "-U", "kb").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "initdb failed, skipping PostgreSQL cases: %v\n%s", err, out)
		cleanup()
		return func() {}
	}
	port := 20000 + rand.IntN(20000)
	opts := fmt.Sprintf("-k %s -p %d -h '' -F", dir, port)
	if out, err := exec.Command(pgctl, "-D", dir+"/data", "-l", dir+"/log", "-o", opts, "-w", "start").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "pg_ctl start failed, skipping PostgreSQL cases: %v\n%s", err, out)
		cleanup()
		return func() {}
	}
	pgDSN = fmt.Sprintf("postgres://kb@/postgres?host=%s&port=%d&sslmode=disable", dir, port)
	return func() {
		exec.Command(pgctl, "-D", dir+"/data", "-m", "immediate", "-w", "stop").Run()
		cleanup()
	}
}

var dbCounter int

// eachBackend runs fn against a fresh SQLite database and, when available, a
// fresh PostgreSQL database.
func eachBackend(t *testing.T, fn func(t *testing.T, db data.Database)) {
	t.Run("sqlite", func(t *testing.T) {
		db, err := data.NewSqliteDatabase(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		if err := data.AddAllTables(db); err != nil {
			t.Fatal(err)
		}
		fn(t, db)
	})
	t.Run("postgres", func(t *testing.T) {
		if pgDSN == "" {
			t.Skip("no PostgreSQL available")
		}
		admin, err := data.NewPostgresDatabase(pgDSN)
		if err != nil {
			t.Fatal(err)
		}
		dbCounter++
		name := fmt.Sprintf("kb_%d_%d", os.Getpid(), dbCounter)
		exec, _, _ := data.Raw(admin)
		if _, err := exec.ExecContext(context.Background(), "CREATE DATABASE "+name); err != nil {
			t.Fatal(err)
		}
		admin.Close()
		db, err := data.NewPostgresDatabase(strings.Replace(pgDSN, "/postgres?", "/"+name+"?", 1))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		if err := data.AddAllTables(db); err != nil {
			t.Fatal(err)
		}
		fn(t, db)
	})
}

// wordEmbedder hashes words into dimensions: texts sharing words are close.
type wordEmbedder struct{ fail bool }

func (w wordEmbedder) vec(text string) []float32 {
	v := make([]float32, EmbeddingDimensions)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		word = strings.Trim(word, ".,!?:;\"'()")
		if len(word) < 3 {
			continue
		}
		h := fnv.New32a()
		h.Write([]byte(word))
		v[h.Sum32()%EmbeddingDimensions] += 1
	}
	return v
}

func (w wordEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	if w.fail {
		return nil, fmt.Errorf("embedder down")
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = w.vec(t)
	}
	return out, nil
}

func (w wordEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	if w.fail {
		return nil, fmt.Errorf("embedder down")
	}
	return w.vec(text), nil
}

func ptr[T any](v T) *T { return &v }

var t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
