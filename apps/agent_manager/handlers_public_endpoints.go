package main

import "net/http"

func (h *Handlers) handlePublicEndpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ingressBaseURL, err := ingressPublicBaseURLWithError()
	if err != nil {
		writeError(w, http.StatusBadRequest, "ingress public url invalid: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ingress_public_url": ingressBaseURL,
		"templates": map[string]any{
			"webhook": ingressBaseURL + "/ingress/webhooks/{provider}/{company_key}/{event_path}",
		},
	})
}
