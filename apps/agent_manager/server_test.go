package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"key": "value"})

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("expected key=value, got %v", result)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "something went wrong")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["error"] != "something went wrong" {
		t.Errorf("expected error message, got %v", result)
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Regular request
	req := httptest.NewRequest("GET", "/api/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS allow-origin header")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// OPTIONS preflight
	req = httptest.NewRequest("OPTIONS", "/api/agents", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for OPTIONS, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected CORS allow-methods header for OPTIONS")
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	handler := recoveryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 after panic, got %d", w.Code)
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["error"] != "internal server error" {
		t.Errorf("expected 'internal server error', got %v", result)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	called := false
	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("expected handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestChainMiddleware(t *testing.T) {
	order := ""

	m1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order += "1"
			next.ServeHTTP(w, r)
		})
	}
	m2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order += "2"
			next.ServeHTTP(w, r)
		})
	}
	m3 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order += "3"
			next.ServeHTTP(w, r)
		})
	}

	handler := chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order += "H"
	}), m1, m2, m3)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if order != "123H" {
		t.Errorf("expected middleware order '123H', got %q", order)
	}
}

func TestNewServer(t *testing.T) {
	server := NewServer(":9999", nil, nil, nil)
	if server.addr != ":9999" {
		t.Errorf("expected addr ':9999', got %q", server.addr)
	}
}

func TestBuildBrokerMuxNilBrokerHasNoRoutes(t *testing.T) {
	mux := buildBrokerMux(nil)
	assertMuxPattern(t, mux, "/broker/v1/tools/anything", "")
	assertMuxPattern(t, mux, "/broker/v1/wallet/address", "")
}

func TestBuildBrokerMuxRegistersConfiguredRoutes(t *testing.T) {
	mux := buildBrokerMux(&BrokerHandlers{
		llm:        &BrokerLLMHandler{},
		wallet:     &BrokerWalletHandler{},
		polymarket: &BrokerPolymarketHandler{},
		email:      &BrokerEmailHandler{},
		search:     &BrokerSearchHandler{},
		telegram:   &BrokerTelegramHandler{},
		tools:      &BrokerToolsHandler{},
		paywall:    &BrokerPaywallHandler{},
		sites:      &BrokerSitesHandler{},
	})

	assertMuxPattern(t, mux, "/broker/v1/llm/generate", "/broker/v1/llm/generate")
	assertMuxPattern(t, mux, "/broker/v1/wallet/address", "/broker/v1/wallet/address")
	assertMuxPattern(t, mux, "/broker/v1/polymarket/search", "/broker/v1/polymarket/search")
	assertMuxPattern(t, mux, "/broker/v1/email/list", "/broker/v1/email/list")
	assertMuxPattern(t, mux, "/broker/v1/search/web", "/broker/v1/search/web")
	assertMuxPattern(t, mux, "/broker/v1/telegram/send", "/broker/v1/telegram/send")
	assertMuxPattern(t, mux, "/broker/v1/tools/get_memory", "/broker/v1/tools/")
	assertMuxPattern(t, mux, "/broker/v1/paywall/create", "/broker/v1/paywall/create")
	assertMuxPattern(t, mux, "/broker/v1/sites/list", "/broker/v1/sites/list")
}

func TestBuildBrokerMuxSkipsNilHandlerGroups(t *testing.T) {
	mux := buildBrokerMux(&BrokerHandlers{
		search: &BrokerSearchHandler{},
	})

	assertMuxPattern(t, mux, "/broker/v1/search/web", "/broker/v1/search/web")
	assertMuxPattern(t, mux, "/broker/v1/wallet/address", "")
	assertMuxPattern(t, mux, "/broker/v1/tools/get_memory", "")
}

func TestBuildHandlerRegistersAPIEndpoints(t *testing.T) {
	server := NewServer(":0", &Handlers{}, nil, nil)
	handler := server.buildHandler()

	paths := []string{
		"/health",
		"/api/agents",
		"/api/companies",
		"/api/mcp-servers",
		"/api/tool-groups",
		"/api/pipelines",
		"/api/builtin-methods/terminal",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Fatalf("expected route %s to be registered, got 404", path)
			}
		})
	}
}

func TestBuildHandlerUnknownRouteReturnsNotFound(t *testing.T) {
	server := NewServer(":0", &Handlers{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/not-a-real-route", nil)
	rec := httptest.NewRecorder()

	server.buildHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown route, got %d", rec.Code)
	}
}

func assertMuxPattern(t *testing.T, mux *http.ServeMux, path string, wantPattern string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	_, gotPattern := mux.Handler(req)
	if gotPattern != wantPattern {
		t.Fatalf("mux pattern for %q = %q, want %q", path, gotPattern, wantPattern)
	}
}
