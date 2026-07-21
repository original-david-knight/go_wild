package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	obj "github.com/original-david-knight/go_wild/objectives"
)

type missionCollectionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, companyID string)
type missionResourceHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string)

type missionActionRoute struct {
	method  string
	handler missionResourceHandlerFunc
}

var missionCollectionHandlers = map[string]missionCollectionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, _ string) {
		h.listMissions(w, r, store, activity)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, companyID string) {
		h.createMission(w, r, store, activity, companyID)
	},
}

var missionResourceHandlers = map[string]missionResourceHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
		h.getMission(w, r, store, activity, missionID)
	},
	http.MethodPut: func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, _ *obj.ActivityStore, missionID string) {
		h.updateMission(w, r, store, missionID)
	},
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
		h.deleteMission(w, r, store, activity, missionID)
	},
}

var missionActionHandlers = map[string]missionActionRoute{
	"pause": {
		method: http.MethodPost,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
			h.pauseMission(w, r, store, activity, missionID)
		},
	},
	"resume": {
		method: http.MethodPost,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
			h.resumeMission(w, r, store, activity, missionID)
		},
	},
	"tree": {
		method: http.MethodGet,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
			h.getMissionTree(w, r, store, activity, missionID)
		},
	},
	"message": {
		method: http.MethodPost,
		handler: func(h *Handlers, w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
			h.sendMissionMessage(w, r, store, activity, missionID)
		},
	},
}

func isMissionAction(action string) bool {
	_, ok := missionActionHandlers[action]
	return ok
}

// handleMissions routes /api/companies/{id}/missions/...
func (h *Handlers) handleMissions(w http.ResponseWriter, r *http.Request, companyID string, subPath string) {
	store := obj.NewObjectiveStore(h.service.db, companyID)
	activity := obj.NewActivityStore(h.service.db, companyID)

	parts := strings.SplitN(strings.TrimPrefix(subPath, "/"), "/", 3)

	// /api/companies/{id}/missions
	if len(parts) == 0 || parts[0] == "" {
		h.dispatchMissionCollectionRoute(w, r, store, activity, companyID)
		return
	}

	// /api/companies/{id}/missions/activity
	if parts[0] == "activity" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.getMissionActivity(w, r, activity)
		return
	}

	// /api/companies/{id}/missions/escalations
	if parts[0] == "escalations" {
		h.dispatchMissionEscalationsRoute(w, r, store, activity, parts)
		return
	}

	// /api/companies/{id}/missions/{mid}
	missionID := parts[0]

	if len(parts) == 1 {
		h.dispatchMissionResourceRoute(w, r, store, activity, missionID)
		return
	}

	// /api/companies/{id}/missions/{mid}/...
	action := parts[1]
	if !isMissionAction(action) {
		writeError(w, http.StatusNotFound, "unknown mission action: "+action)
		return
	}
	route, ok := missionActionHandlers[action]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown mission action: "+action)
		return
	}
	if r.Method != route.method {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	route.handler(h, w, r, store, activity, missionID)
}

func (h *Handlers) dispatchMissionCollectionRoute(
	w http.ResponseWriter,
	r *http.Request,
	store *obj.ObjectiveStore,
	activity *obj.ActivityStore,
	companyID string,
) {
	handler, ok := missionCollectionHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, store, activity, companyID)
}

func (h *Handlers) dispatchMissionResourceRoute(
	w http.ResponseWriter,
	r *http.Request,
	store *obj.ObjectiveStore,
	activity *obj.ActivityStore,
	missionID string,
) {
	handler, ok := missionResourceHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, store, activity, missionID)
}

func (h *Handlers) dispatchMissionEscalationsRoute(
	w http.ResponseWriter,
	r *http.Request,
	store *obj.ObjectiveStore,
	activity *obj.ActivityStore,
	parts []string,
) {
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.getMissionEscalations(w, r, store)
		return
	}
	// /api/companies/{id}/missions/escalations/{eid}/resolve
	if len(parts) >= 2 {
		escID := parts[1]
		escSub := ""
		if len(parts) >= 3 {
			escSub = parts[2]
		}
		if escSub == "resolve" && r.Method == http.MethodPost {
			h.resolveMissionEscalation(w, r, store, activity, escID)
			return
		}
	}
	writeError(w, http.StatusNotFound, "not found")
}

// GET /api/companies/{id}/missions — list root objectives for this company
func (h *Handlers) listMissions(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore) {
	ctx := r.Context()
	roots, err := store.GetRoots(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var rollups []*obj.StatusRollup
	for _, o := range roots {
		rollup, err := obj.GetObjectiveStatus(ctx, store, activity, o.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rollups = append(rollups, rollup)
	}

	if rollups == nil {
		rollups = []*obj.StatusRollup{}
	}
	writeJSON(w, http.StatusOK, rollups)
}

// POST /api/companies/{id}/missions — create a new mission
func (h *Handlers) createMission(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, companyID string) {
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

	mission := &obj.Objective{
		Title:       input.Title,
		Description: input.Description,
		Priority:    1,
	}
	mission.ScheduleType = obj.ScheduleContinuous
	if err := store.Create(r.Context(), mission); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	activity.LogEvent(r.Context(), &obj.ActivityEvent{
		ObjectiveID: mission.ID,
		EventType:   "objective_created",
		Severity:    obj.SeverityInfo,
		Summary:     fmt.Sprintf("Created mission: %s", mission.Title),
	})

	writeJSON(w, http.StatusCreated, mission)
}

// GET /api/companies/{id}/missions/{mid} — single objective with children + activity
func (h *Handlers) getMission(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
	ctx := r.Context()

	mission, err := store.Get(ctx, missionID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mission not found: %s", missionID))
		return
	}

	children, err := store.GetChildren(ctx, missionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	events, err := activity.GetEventsForTree(ctx, store, missionID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if children == nil {
		children = []*obj.Objective{}
	}
	if events == nil {
		events = []*obj.ActivityEvent{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"objective":       mission,
		"children":        children,
		"recent_activity": events,
	})
}

// PUT /api/companies/{id}/missions/{mid} — update mission fields
func (h *Handlers) updateMission(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, missionID string) {
	ctx := r.Context()
	mission, err := store.Get(ctx, missionID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mission not found: %s", missionID))
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
		mission.Title = *input.Title
	}
	if input.Description != nil {
		mission.Description = *input.Description
	}
	if input.Priority != nil {
		mission.Priority = *input.Priority
	}

	if err := store.Update(ctx, mission); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mission)
}

// DELETE /api/companies/{id}/missions/{mid} — delete mission tree
func (h *Handlers) deleteMission(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
	ctx := r.Context()
	mission, err := store.Get(ctx, missionID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mission not found: %s", missionID))
		return
	}

	if err := store.Delete(ctx, missionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	activity.LogEvent(ctx, &obj.ActivityEvent{
		ObjectiveID: missionID,
		EventType:   "objective_deleted",
		Severity:    obj.SeverityInfo,
		Summary:     fmt.Sprintf("Deleted mission: %s", mission.Title),
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /api/companies/{id}/missions/{mid}/pause
func (h *Handlers) pauseMission(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
	ctx := r.Context()
	mission, err := store.Get(ctx, missionID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mission not found: %s", missionID))
		return
	}

	mission.Status = obj.StatusPaused
	if err := store.Update(ctx, mission); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	activity.LogEvent(ctx, &obj.ActivityEvent{
		ObjectiveID: missionID,
		EventType:   "objective_paused",
		Severity:    obj.SeverityInfo,
		Summary:     fmt.Sprintf("Paused mission: %s", mission.Title),
	})

	writeJSON(w, http.StatusOK, mission)
}

// POST /api/companies/{id}/missions/{mid}/resume
func (h *Handlers) resumeMission(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
	ctx := r.Context()
	mission, err := store.Get(ctx, missionID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mission not found: %s", missionID))
		return
	}

	mission.Status = obj.StatusActive
	mission.CooldownUntil = time.Time{}
	if err := store.Update(ctx, mission); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	activity.LogEvent(ctx, &obj.ActivityEvent{
		ObjectiveID: missionID,
		EventType:   "objective_resumed",
		Severity:    obj.SeverityInfo,
		Summary:     fmt.Sprintf("Resumed mission: %s", mission.Title),
	})

	writeJSON(w, http.StatusOK, mission)
}

// GET /api/companies/{id}/missions/{mid}/tree — tree status rollup
func (h *Handlers) getMissionTree(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
	ctx := r.Context()
	rollups, err := obj.GetTreeStatus(ctx, store, activity, missionID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("tree not found: %s", missionID))
		return
	}
	writeJSON(w, http.StatusOK, rollups)
}

// GET /api/companies/{id}/missions/activity — recent activity for company
func (h *Handlers) getMissionActivity(w http.ResponseWriter, r *http.Request, activity *obj.ActivityStore) {
	ctx := r.Context()
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events, err := activity.GetRecentEvents(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		events = []*obj.ActivityEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// GET /api/companies/{id}/missions/escalations — pending escalations for company
func (h *Handlers) getMissionEscalations(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore) {
	ctx := r.Context()

	// Get all objectives for this company, then find escalations
	roots, err := store.GetRoots(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var allEscalations []*obj.Escalation
	for _, root := range roots {
		tree, err := store.GetTree(ctx, root.ID)
		if err != nil {
			continue
		}
		for _, o := range tree {
			escs := store.GetEscalations(ctx, o.ID)
			allEscalations = append(allEscalations, escs...)
		}
	}

	if allEscalations == nil {
		allEscalations = []*obj.Escalation{}
	}
	writeJSON(w, http.StatusOK, allEscalations)
}

// POST /api/companies/{id}/missions/escalations/{eid}/resolve
func (h *Handlers) resolveMissionEscalation(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, escID string) {
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
	esc, err := store.ResolveEscalation(ctx, escID, input.Resolution)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("escalation not found: %s", escID))
		return
	}

	activity.LogEvent(ctx, &obj.ActivityEvent{
		ObjectiveID: esc.ObjectiveID,
		EventType:   "escalation_resolved",
		Severity:    obj.SeverityInfo,
		Summary:     fmt.Sprintf("Resolved escalation: %s", input.Resolution),
	})

	writeJSON(w, http.StatusOK, esc)
}

// POST /api/companies/{id}/missions/{mid}/message — send guidance to a mission
func (h *Handlers) sendMissionMessage(w http.ResponseWriter, r *http.Request, store *obj.ObjectiveStore, activity *obj.ActivityStore, missionID string) {
	var input struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	ctx := r.Context()

	// Verify mission exists
	mission, err := store.Get(ctx, missionID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mission not found: %s", missionID))
		return
	}

	// Store as a user_directive activity event on the root mission
	ev := &obj.ActivityEvent{
		ObjectiveID: mission.ID,
		EventType:   "user_directive",
		Severity:    obj.SeverityInfo,
		Summary:     content,
	}

	// Persist the directive first so it's available as planner context.
	if err := activity.LogEvent(ctx, ev); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Guidance should wake completed/failed missions so the scheduler can continue.
	// Paused missions remain paused so explicit pause intent is preserved.
	if mission.Status == obj.StatusCompleted || mission.Status == obj.StatusFailed {
		mission.Status = obj.StatusActive
		mission.CompletedAt = time.Time{}
		mission.CooldownUntil = time.Time{}
		mission.LastResult = "Mission resumed from user guidance"
		if err := store.Update(ctx, mission); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		activity.LogEvent(ctx, &obj.ActivityEvent{
			ObjectiveID: mission.ID,
			EventType:   "objective_resumed",
			Severity:    obj.SeverityInfo,
			Summary:     fmt.Sprintf("Resumed mission from user guidance: %s", mission.Title),
		})
	} else if mission.Status != obj.StatusPaused && !mission.CooldownUntil.IsZero() {
		// Clear cooldown for active/pending/blocked missions so guidance is picked up promptly.
		mission.CooldownUntil = time.Time{}
		if err := store.Update(ctx, mission); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, ev)
}
