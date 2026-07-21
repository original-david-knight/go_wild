package main

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

// SpendHandler handles the spend API endpoints for the web UI.
type SpendHandler struct {
	db gowild_data.Database
}

// NewSpendHandler creates a new spend handler.
func NewSpendHandler(db gowild_data.Database) *SpendHandler {
	return &SpendHandler{db: db}
}

// getAgentSpend returns spend data for an agent.
// GET /api/agents/{id}/spend?category=ads
func (sh *SpendHandler) getAgentSpend(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	svc := data.NewAgentService(sh.db, agentID)

	category := r.URL.Query().Get("category")

	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	endOfDay := startOfDay.Add(24 * time.Hour)

	entries, err := svc.GetSpendHistory(r.Context(), startOfDay, endOfDay)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type spendSummary struct {
		Category   string  `json:"category"`
		TotalSpend float64 `json:"total_spend"`
		DailyLimit float64 `json:"daily_limit"`
	}

	totals := make(map[string]float64)
	for _, e := range entries {
		if category == "" || e.Category == category {
			totals[e.Category] += e.Amount
		}
	}

	var summaries []spendSummary
	for cat, total := range totals {
		limit, _ := svc.GetSpendLimit(r.Context(), cat)
		if limit == 0 {
			limit = defaultDailyLimits[cat]
		}
		summaries = append(summaries, spendSummary{
			Category:   cat,
			TotalSpend: total,
			DailyLimit: limit,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":  agentID,
		"date":      startOfDay.Format("2006-01-02"),
		"summaries": summaries,
		"entries":   entries,
	})
}

// setSpendLimit sets a spend limit for an agent.
// POST /api/agents/{id}/spend/limits
func (sh *SpendHandler) setSpendLimit(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var input struct {
		Category   string  `json:"category"`
		DailyLimit float64 `json:"daily_limit"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if input.Category == "" {
		writeError(w, http.StatusBadRequest, "category is required")
		return
	}
	if input.DailyLimit < 0 {
		writeError(w, http.StatusBadRequest, "daily_limit must be non-negative")
		return
	}

	svc := data.NewAgentService(sh.db, agentID)
	if err := svc.SetSpendLimit(r.Context(), input.Category, input.DailyLimit); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":    agentID,
		"category":    input.Category,
		"daily_limit": input.DailyLimit,
	})
}
