package main

import "net/http"

func (h *Handlers) handleBuiltinMethodsTerminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.pipelineEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "pipeline engine not configured")
		return
	}
	h.pipelineEngine.getBuiltinTerminalHub().ServeWS(w, r)
}
