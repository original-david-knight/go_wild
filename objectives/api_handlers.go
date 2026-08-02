package objectives

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// GET /health
func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/objectives — list root objectives with status rollups
func (s *APIServer) handleGetObjectives(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roots, err := s.store.GetRoots(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var rollups []*StatusRollup
	for _, obj := range roots {
		rollup, err := GetObjectiveStatus(ctx, s.store, s.activity, obj.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rollups = append(rollups, rollup)
	}

	if rollups == nil {
		rollups = []*StatusRollup{}
	}
	writeJSON(w, http.StatusOK, rollups)
}

// GET /api/objectives/{id} — single objective with children + recent activity
func (s *APIServer) handleGetObjective(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing objective id")
		return
	}

	ctx := r.Context()

	obj, err := s.store.Get(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("objective not found: %s", id))
		return
	}

	children, err := s.store.GetChildren(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	events, err := s.activity.GetEventsForTree(ctx, s.store, id, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if children == nil {
		children = []*Objective{}
	}
	if events == nil {
		events = []*ActivityEvent{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"objective":       obj,
		"children":        children,
		"recent_activity": events,
	})
}

// POST /api/objectives — create a new mission
func (s *APIServer) handleCreateObjective(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if input.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	obj := &Objective{
		Title:        input.Title,
		Description:  input.Description,
		Priority:     1,
		ScheduleType: ScheduleContinuous,
	}
	if err := s.store.Create(r.Context(), obj); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.activity.LogEvent(r.Context(), &ActivityEvent{
		ObjectiveID: obj.ID,
		EventType:   "objective_created",
		Severity:    SeverityInfo,
		Summary:     fmt.Sprintf("Created objective: %s", obj.Title),
	})

	if s.hub != nil {
		s.hub.Broadcast(map[string]any{
			"type":      "objective_created",
			"objective": obj,
		})
	}

	writeJSON(w, http.StatusCreated, obj)
}

// PUT /api/objectives/{id} — update objective fields
func (s *APIServer) handleUpdateObjective(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing objective id")
		return
	}

	ctx := r.Context()
	obj, err := s.store.Get(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("objective not found: %s", id))
		return
	}

	var input struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Priority    *int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if input.Title != nil {
		obj.Title = *input.Title
	}
	if input.Description != nil {
		obj.Description = *input.Description
	}
	if input.Priority != nil {
		obj.Priority = *input.Priority
	}

	if err := s.store.Update(ctx, obj); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if s.hub != nil {
		s.hub.Broadcast(map[string]any{
			"type":      "objective_updated",
			"objective": obj,
		})
	}

	writeJSON(w, http.StatusOK, obj)
}

// DELETE /api/objectives/{id} — delete an objective and its children
func (s *APIServer) handleDeleteObjective(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing objective id")
		return
	}

	ctx := r.Context()
	obj, err := s.store.Get(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("objective not found: %s", id))
		return
	}

	if err := s.store.Delete(ctx, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: id,
		EventType:   "objective_deleted",
		Severity:    SeverityInfo,
		Summary:     fmt.Sprintf("Deleted objective: %s", obj.Title),
	})

	if s.hub != nil {
		s.hub.Broadcast(map[string]any{
			"type": "objective_deleted",
			"id":   id,
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /api/objectives/{id}/pause — set status to paused
func (s *APIServer) handlePauseObjective(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing objective id")
		return
	}

	ctx := r.Context()
	obj, err := s.store.Get(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("objective not found: %s", id))
		return
	}

	obj.Status = StatusPaused
	if err := s.store.Update(ctx, obj); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: id,
		EventType:   "objective_paused",
		Severity:    SeverityInfo,
		Summary:     fmt.Sprintf("Paused objective: %s", obj.Title),
	})

	if s.hub != nil {
		s.hub.Broadcast(map[string]any{
			"type":      "objective_paused",
			"objective": obj,
		})
	}

	writeJSON(w, http.StatusOK, obj)
}

// POST /api/objectives/{id}/resume — set status to active
func (s *APIServer) handleResumeObjective(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing objective id")
		return
	}

	ctx := r.Context()
	obj, err := s.store.Get(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("objective not found: %s", id))
		return
	}

	obj.Status = StatusActive
	obj.CooldownUntil = time.Time{} // clear cooldown
	if err := s.store.Update(ctx, obj); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: id,
		EventType:   "objective_resumed",
		Severity:    SeverityInfo,
		Summary:     fmt.Sprintf("Resumed objective: %s", obj.Title),
	})

	if s.hub != nil {
		s.hub.Broadcast(map[string]any{
			"type":      "objective_resumed",
			"objective": obj,
		})
	}

	writeJSON(w, http.StatusOK, obj)
}

// GET /api/activity — recent activity events
func (s *APIServer) handleGetActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	objectiveID := r.URL.Query().Get("objective_id")

	var events []*ActivityEvent
	var err error

	if objectiveID != "" {
		events, err = s.activity.GetEvents(ctx, objectiveID, limit)
	} else {
		events, err = s.activity.GetRecentEvents(ctx, limit)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if events == nil {
		events = []*ActivityEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// GET /api/escalations — list pending escalations
func (s *APIServer) handleGetEscalations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	results, err := s.store.db.Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"status": string(EscalationPending)},
		OrderBy:   "created_at",
		OrderDesc: true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	escalations := make([]*Escalation, 0, len(results))
	for _, r := range results {
		if e, ok := r.(*Escalation); ok {
			escalations = append(escalations, e)
		}
	}
	writeJSON(w, http.StatusOK, escalations)
}

// POST /api/escalations/{id}/resolve — resolve an escalation
func (s *APIServer) handleResolveEscalation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing escalation id")
		return
	}

	var input struct {
		Resolution string `json:"resolution"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if input.Resolution == "" {
		writeError(w, http.StatusBadRequest, "resolution is required")
		return
	}

	ctx := r.Context()

	var esc Escalation
	if err := s.store.db.Table(Escalation{}).Get(ctx, id, &esc); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("escalation not found: %s", id))
		return
	}

	esc.Status = EscalationResolved
	esc.Resolution = input.Resolution
	esc.ResolvedAt = time.Now().UTC()

	if err := s.store.db.Table(Escalation{}).Update(ctx, &esc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: esc.ObjectiveID,
		EventType:   "escalation_resolved",
		Severity:    SeverityInfo,
		Summary:     fmt.Sprintf("Resolved escalation: %s", input.Resolution),
	})

	// If all escalations for this objective are resolved, unblock it
	remaining, _ := s.store.db.Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"objective_id": esc.ObjectiveID, "status": string(EscalationPending)},
		Limit: 1,
	})
	if len(remaining) == 0 {
		obj, err := s.store.Get(ctx, esc.ObjectiveID)
		if err == nil && obj.Status == StatusBlocked {
			obj.Status = StatusPending
			obj.CooldownUntil = time.Time{}
			s.store.Update(ctx, obj)
			s.activity.LogEvent(ctx, &ActivityEvent{
				ObjectiveID: obj.ID,
				EventType:   "objective_unblocked",
				Severity:    SeverityInfo,
				Summary:     "All clarifications answered — objective unblocked",
			})
		}
	}

	if s.hub != nil {
		s.hub.Broadcast(map[string]any{
			"type":       "escalation_resolved",
			"escalation": &esc,
		})
	}

	writeJSON(w, http.StatusOK, &esc)
}

// GET /api/status — system health
func (s *APIServer) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	counts := make(map[string]int)
	for _, status := range []ObjectiveStatus{StatusPending, StatusActive, StatusBlocked, StatusCompleted, StatusFailed, StatusPaused} {
		objs, err := s.store.GetByStatus(ctx, status)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		counts[string(status)] = len(objs)
	}

	allEvents, err := s.activity.GetRecentEvents(ctx, 1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	totalObjectives := 0
	for _, c := range counts {
		totalObjectives += c
	}

	result := map[string]any{
		"objective_counts":  counts,
		"total_objectives":  totalObjectives,
		"has_recent_events": len(allEvents) > 0,
		"uptime_seconds":    int(time.Since(s.startTime).Seconds()),
		"ws_clients":        s.hub.ClientCount(),
	}

	writeJSON(w, http.StatusOK, result)
}

// handleGetStatusForObjective returns status for a specific objective within a tree context.
func (s *APIServer) handleGetTreeStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing objective id")
		return
	}

	ctx := r.Context()
	rollups, err := GetTreeStatus(ctx, s.store, s.activity, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("tree not found: %s", id))
		return
	}

	writeJSON(w, http.StatusOK, rollups)
}
