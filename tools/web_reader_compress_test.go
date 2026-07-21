package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompressContentSkipsWhenDeadlineTooSoon(t *testing.T) {
	var calls int32
	w := NewWebReaderTools(func(ctx context.Context, markdown string) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "compressed", nil
	})

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	defer cancel()

	got, err := w.compressContent(ctx, "original markdown")
	if err != nil {
		t.Fatalf("compressContent() error = %v", err)
	}
	if got != "original markdown" {
		t.Fatalf("compressContent() = %q, want original content", got)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("compress function was called, want skipped")
	}
}

func TestCompressContentDisablesAfterTimeout(t *testing.T) {
	var calls int32
	w := NewWebReaderTools(func(ctx context.Context, markdown string) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", context.DeadlineExceeded
	})

	got, err := w.compressContent(context.Background(), "first content")
	if err != nil {
		t.Fatalf("compressContent() error = %v", err)
	}
	if got != "first content" {
		t.Fatalf("compressContent() = %q, want original content", got)
	}

	got, err = w.compressContent(context.Background(), "second content")
	if err != nil {
		t.Fatalf("compressContent() second call error = %v", err)
	}
	if got != "second content" {
		t.Fatalf("compressContent() second call = %q, want original content", got)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("compress function calls = %d, want 1 due to cooldown", calls)
	}
}

func TestCompressContentPropagatesNonTimeoutError(t *testing.T) {
	wantErr := errors.New("boom")
	w := NewWebReaderTools(func(ctx context.Context, markdown string) (string, error) {
		return "", wantErr
	})

	got, err := w.compressContent(context.Background(), "content")
	if !errors.Is(err, wantErr) {
		t.Fatalf("compressContent() error = %v, want %v", err, wantErr)
	}
	if got != "" {
		t.Fatalf("compressContent() = %q, want empty on error", got)
	}
}

func TestCompressContentFallsBackOnEmptyResponse(t *testing.T) {
	w := NewWebReaderTools(func(ctx context.Context, markdown string) (string, error) {
		return "   \n", nil
	})

	got, err := w.compressContent(context.Background(), "original markdown")
	if err != nil {
		t.Fatalf("compressContent() error = %v", err)
	}
	if got != "original markdown" {
		t.Fatalf("compressContent() = %q, want original content", got)
	}
}
