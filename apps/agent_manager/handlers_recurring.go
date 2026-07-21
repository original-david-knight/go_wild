package main

import (
	"encoding/json"
	"net/http"
	"time"
)

type recurringCollectionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string)
type recurringTaskHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, taskID string)

var recurringCollectionHandlers = map[string]recurringCollectionHandlerFunc{
	http.MethodGet: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
		h.listRecurringTasks(w, r, agentID)
	},
	http.MethodPost: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID string) {
		h.createRecurringTask(w, r, agentID)
	},
}

var recurringTaskHandlers = map[string]recurringTaskHandlerFunc{
	http.MethodPut: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, taskID string) {
		h.updateRecurringTask(w, r, agentID, taskID)
	},
	http.MethodDelete: func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, taskID string) {
		h.deleteRecurringTask(w, r, agentID, taskID)
	},
}

func isRecurringCollectionMethod(method string) bool {
	_, ok := recurringCollectionHandlers[method]
	return ok
}

func isRecurringTaskMethod(method string) bool {
	_, ok := recurringTaskHandlers[method]
	return ok
}

func (h *Handlers) handleRecurringTasks(w http.ResponseWriter, r *http.Request, agentID, taskID string) {
	// Operations on a specific task
	if taskID != "" {
		if !isRecurringTaskMethod(r.Method) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler, ok := recurringTaskHandlers[r.Method]
		if !ok {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler(h, w, r, agentID, taskID)
		return
	}

	// Operations on the collection
	if !isRecurringCollectionMethod(r.Method) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler, ok := recurringCollectionHandlers[r.Method]
	if !ok {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	handler(h, w, r, agentID)
}

func (h *Handlers) listRecurringTasks(w http.ResponseWriter, r *http.Request, agentID string) {
	tasks, err := h.service.GetRecurringTasks(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get recurring tasks: "+err.Error())
		return
	}

	// Convert to response format with next_due calculation
	taskList := make([]map[string]any, len(tasks))
	for i, t := range tasks {
		nextDue := t.LastCreatedAt.Add(time.Duration(t.IntervalMinutes) * time.Minute)
		taskList[i] = map[string]any{
			"id":               t.ID,
			"description":      t.Description,
			"interval_minutes": t.IntervalMinutes,
			"last_created_at":  t.LastCreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			"next_due":         nextDue.Format("2006-01-02T15:04:05Z07:00"),
			"created_at":       t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"tasks": taskList})
}

type CreateRecurringTaskRequest struct {
	Description     string `json:"description"`
	IntervalMinutes int    `json:"interval_minutes"`
}

func (h *Handlers) createRecurringTask(w http.ResponseWriter, r *http.Request, agentID string) {
	var req CreateRecurringTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Description == "" {
		writeError(w, http.StatusBadRequest, "description is required")
		return
	}
	if req.IntervalMinutes <= 0 {
		writeError(w, http.StatusBadRequest, "interval_minutes must be positive")
		return
	}

	task, err := h.service.AddRecurringTask(r.Context(), agentID, req.Description, req.IntervalMinutes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recurring task: "+err.Error())
		return
	}

	nextDue := task.LastCreatedAt.Add(time.Duration(task.IntervalMinutes) * time.Minute)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":               task.ID,
		"description":      task.Description,
		"interval_minutes": task.IntervalMinutes,
		"last_created_at":  task.LastCreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"next_due":         nextDue.Format("2006-01-02T15:04:05Z07:00"),
		"created_at":       task.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

type UpdateRecurringTaskRequest struct {
	Description     string `json:"description"`
	IntervalMinutes int    `json:"interval_minutes"`
}

func (h *Handlers) updateRecurringTask(w http.ResponseWriter, r *http.Request, agentID, taskID string) {
	var req UpdateRecurringTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Description == "" {
		writeError(w, http.StatusBadRequest, "description is required")
		return
	}
	if req.IntervalMinutes <= 0 {
		writeError(w, http.StatusBadRequest, "interval_minutes must be positive")
		return
	}

	task, err := h.service.UpdateRecurringTask(r.Context(), agentID, taskID, req.Description, req.IntervalMinutes)
	if err != nil {
		writeError(w, http.StatusNotFound, "recurring task not found: "+err.Error())
		return
	}

	nextDue := task.LastCreatedAt.Add(time.Duration(task.IntervalMinutes) * time.Minute)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":               task.ID,
		"description":      task.Description,
		"interval_minutes": task.IntervalMinutes,
		"last_created_at":  task.LastCreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"next_due":         nextDue.Format("2006-01-02T15:04:05Z07:00"),
		"created_at":       task.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *Handlers) deleteRecurringTask(w http.ResponseWriter, r *http.Request, agentID, taskID string) {
	if err := h.service.DeleteRecurringTask(r.Context(), agentID, taskID); err != nil {
		writeError(w, http.StatusNotFound, "recurring task not found: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
