package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]string{"ok": "yes"})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
	if got := rec.Body.String(); got != "{\"ok\":\"yes\"}\n" {
		t.Errorf("body = %q, want %q", got, "{\"ok\":\"yes\"}\n")
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusTeapot, "no coffee here")

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if got := rec.Body.String(); got != "{\"error\":\"no coffee here\"}\n" {
		t.Errorf("body = %q, want %q", got, "{\"error\":\"no coffee here\"}\n")
	}
}

func TestDecodeJSONSuccess(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"a"}`))

	var dst struct {
		Name string `json:"name"`
	}
	if !DecodeJSON(rec, req, &dst, "unused hint") {
		t.Fatal("DecodeJSON = false, want true")
	}
	if dst.Name != "a" {
		t.Errorf("decoded name = %q, want %q", dst.Name, "a")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty on success", rec.Body.String())
	}
}

func TestDecodeJSONMalformed(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not json"))

	var dst struct{}
	if DecodeJSON(rec, req, &dst, `body must be {"name": "…"}`) {
		t.Fatal("DecodeJSON = true, want false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	want := "{\"error\":\"body must be {\\\"name\\\": \\\"…\\\"}\"}\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
