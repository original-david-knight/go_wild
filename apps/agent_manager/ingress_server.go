package main

import (
	"fmt"
	"net/http"
	"time"
)

// IngressServer exposes only webhook ingress routes.
type IngressServer struct {
	addr          string
	handler       *Handlers
	webhookRouter *WebhookRouter
}

// NewIngressServer creates a new ingress-only HTTP server.
func NewIngressServer(addr string, handler *Handlers, webhookRouter *WebhookRouter) *IngressServer {
	return &IngressServer{addr: addr, handler: handler, webhookRouter: webhookRouter}
}

// ListenAndServe starts the ingress-only server.
func (s *IngressServer) ListenAndServe() error {
	server := &http.Server{
		Addr:         s.addr,
		Handler:      s.buildHandler(),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}
	fmt.Printf("Ingress server listening on %s\n", s.addr)
	return server.ListenAndServe()
}

func (s *IngressServer) buildHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	if s.webhookRouter != nil {
		mux.HandleFunc("/ingress/webhooks/", s.webhookRouter.HandleIngressWebhook)
	}
	return chain(mux, loggingMiddleware, recoveryMiddleware, corsMiddleware)
}
