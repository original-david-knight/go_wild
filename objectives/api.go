package objectives

import (
	"net/http"
	"time"
)

// APIServer serves the REST API and WebSocket endpoints for the objectives system.
type APIServer struct {
	store     *ObjectiveStore
	activity  *ActivityStore
	hub       *WSHub
	mux       *http.ServeMux
	startTime time.Time
}

// NewAPIServer creates a new API server with the given stores.
func NewAPIServer(store *ObjectiveStore, activity *ActivityStore) *APIServer {
	s := &APIServer{
		store:     store,
		activity:  activity,
		hub:       NewWSHub(),
		mux:       http.NewServeMux(),
		startTime: time.Now(),
	}
	s.setupRoutes()
	go s.hub.Run()
	return s
}

// setupRoutes registers all HTTP handlers on the mux.
func (s *APIServer) setupRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /api/objectives", s.handleGetObjectives)
	s.mux.HandleFunc("GET /api/objectives/{id}", s.handleGetObjective)
	s.mux.HandleFunc("POST /api/objectives", s.handleCreateObjective)
	s.mux.HandleFunc("PUT /api/objectives/{id}", s.handleUpdateObjective)
	s.mux.HandleFunc("POST /api/objectives/{id}/pause", s.handlePauseObjective)
	s.mux.HandleFunc("POST /api/objectives/{id}/resume", s.handleResumeObjective)
	s.mux.HandleFunc("DELETE /api/objectives/{id}", s.handleDeleteObjective)
	s.mux.HandleFunc("GET /api/objectives/{id}/tree", s.handleGetTreeStatus)
	s.mux.HandleFunc("GET /api/activity", s.handleGetActivity)
	s.mux.HandleFunc("GET /api/escalations", s.handleGetEscalations)
	s.mux.HandleFunc("POST /api/escalations/{id}/resolve", s.handleResolveEscalation)
	s.mux.HandleFunc("GET /api/status", s.handleGetStatus)
	s.mux.HandleFunc("GET /api/stream", s.hub.handleWebSocket)

	// Dashboard UI (served at root)
	s.mux.Handle("/", DashboardHandler())
}

// ServeHTTP implements the http.Handler interface.
func (s *APIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ListenAndServe starts the HTTP server on the given address.
func (s *APIServer) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s)
}
