package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
)

func (h *BrokerPolymarketHandler) handleAddMarketNote(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}
	if method, disabled := h.currentExecutionDisablesMarketNotes(r.Context()); disabled {
		writeError(w, http.StatusForbidden, "market notes are disabled for method "+method)
		return
	}

	var input tools.PolymarketAddMarketNoteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	member, err := h.service.GetCompanyMemberForAgent(r.Context(), agentID)
	if err != nil {
		log.Printf("Broker market note error for agent %s: %v", agentID, err)
		writeError(w, http.StatusInternalServerError, "failed to resolve company: "+err.Error())
		return
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		writeError(w, http.StatusForbidden, "market notes require company membership")
		return
	}
	companyID := strings.TrimSpace(member.CompanyID)

	note, err := data.AddMarketNote(r.Context(), h.service.db, companyID, agentID, input.ConditionID, input.Content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"note":       marketNoteToMap(note),
		"company_id": companyID,
	})
}

func (h *BrokerPolymarketHandler) handleListMarketNotes(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}
	if method, disabled := h.currentExecutionDisablesMarketNotes(r.Context()); disabled {
		writeError(w, http.StatusForbidden, "market notes are disabled for method "+method)
		return
	}

	var input tools.PolymarketListMarketNotesInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	member, err := h.service.GetCompanyMemberForAgent(r.Context(), agentID)
	if err != nil {
		log.Printf("Broker market note error for agent %s: %v", agentID, err)
		writeError(w, http.StatusInternalServerError, "failed to resolve company: "+err.Error())
		return
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		writeError(w, http.StatusForbidden, "market notes require company membership")
		return
	}
	companyID := strings.TrimSpace(member.CompanyID)

	notes, err := data.ListMarketNotes(r.Context(), h.service.db, companyID, input.ConditionID, input.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := make([]map[string]any, len(notes))
	for i, n := range notes {
		out[i] = marketNoteToMap(n)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"notes":      out,
		"company_id": companyID,
	})
}

func marketNoteToMap(n *data.MarketNote) map[string]any {
	out := map[string]any{
		"id":                  n.ID,
		"company_id":          n.CompanyID,
		"condition_id":        n.ConditionID,
		"content":             n.Content,
		"created_by_agent_id": n.CreatedByAgentID,
		"created_at":          n.CreatedAt,
	}
	if metadata := data.ParseMarketNoteMetadata(n); metadata != nil {
		out["metadata"] = metadata
	}
	return out
}
