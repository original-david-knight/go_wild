package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// SSE writes server-sent events onto an HTTP response, one flushed frame per
// event — each event reaches the client as it happens, never buffered
// whole-response.
type SSE struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// StartSSE prepares w for a server-sent-events stream: it sets the stream
// headers, writes the 200 status, and flushes once so the client sees the
// stream open before the first event arrives. It returns an error when w is
// not an http.Flusher, because a stream that cannot flush cannot stream.
func StartSSE(w http.ResponseWriter) (*SSE, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming unsupported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &SSE{w: w, flusher: flusher}, nil
}

// Send marshals v and writes it as one data-only frame, flushed immediately.
// A marshal error is returned without writing a frame; a write error is
// returned as-is.
func (s *SSE) Send(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", raw); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
