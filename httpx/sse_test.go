package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// noFlush hides the recorder's Flush method so the value does not satisfy
// http.Flusher: interface embedding promotes only the interface's own methods.
type noFlush struct {
	http.ResponseWriter
}

func TestStartSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	s, err := StartSSE(rec)
	if err != nil {
		t.Fatalf("StartSSE: %v", err)
	}
	if s == nil {
		t.Fatal("StartSSE returned a nil writer")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", got, "text/event-stream")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want %q", got, "no")
	}
	if !rec.Flushed {
		t.Error("StartSSE did not flush the opening of the stream")
	}
}

func TestStartSSENonFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	s, err := StartSSE(noFlush{rec})
	if err == nil {
		t.Fatal("StartSSE on a non-Flusher: err = nil, want error")
	}
	if s != nil {
		t.Fatal("StartSSE on a non-Flusher returned a writer")
	}
	if len(rec.Header()) != 0 {
		t.Errorf("headers = %v, want none set on failure", rec.Header())
	}
}

func TestSendFrames(t *testing.T) {
	rec := httptest.NewRecorder()
	s, err := StartSSE(rec)
	if err != nil {
		t.Fatalf("StartSSE: %v", err)
	}
	if err := s.Send(map[string]int{"a": 1}); err != nil {
		t.Fatalf("Send first frame: %v", err)
	}
	if err := s.Send(map[string]int{"b": 2}); err != nil {
		t.Fatalf("Send second frame: %v", err)
	}
	want := "data: {\"a\":1}\n\ndata: {\"b\":2}\n\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestSendMarshalError(t *testing.T) {
	rec := httptest.NewRecorder()
	s, err := StartSSE(rec)
	if err != nil {
		t.Fatalf("StartSSE: %v", err)
	}
	if err := s.Send(make(chan int)); err == nil {
		t.Fatal("Send of an unmarshalable value: err = nil, want error")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want no frame after a marshal error", rec.Body.String())
	}
}
