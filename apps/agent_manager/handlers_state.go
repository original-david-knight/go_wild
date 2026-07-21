package main

import (
	"net/http"
	"strconv"
	"strings"
)

// Agent state handlers

func (h *Handlers) getMemory(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	memory, err := h.service.GetMemory(r.Context(), agentID)
	if err != nil || memory == nil {
		writeJSON(w, http.StatusOK, map[string]any{"content": ""})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content":    memory.Content,
		"updated_at": memory.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *Handlers) getArchive(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	entries, err := h.service.GetArchiveEntries(r.Context(), agentID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}})
		return
	}

	result := make([]map[string]any, len(entries))
	for i, e := range entries {
		result[i] = map[string]any{
			"id":         e.ID,
			"summary":    e.Summary,
			"tags":       e.Tags,
			"content":    e.Content,
			"created_at": e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"entries": result})
}

func (h *Handlers) getReport(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	html, updatedAt, err := h.service.GetReportHTML(r.Context(), agentID)
	if err != nil || html == "" {
		writeJSON(w, http.StatusOK, map[string]any{"html": ""})
		return
	}

	resp := map[string]any{"html": html}
	if !updatedAt.IsZero() {
		resp["updated_at"] = updatedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) getSoul(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	soul, err := h.service.GetSoul(r.Context(), agentID)
	if err != nil || soul == nil {
		writeJSON(w, http.StatusOK, map[string]any{"content": ""})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content":    soul.Content,
		"updated_at": soul.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"updated_by": soul.UpdatedBy,
	})
}

func (h *Handlers) getTasks(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tasks, err := h.service.GetPendingTasks(r.Context(), agentID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"tasks": []any{}})
		return
	}

	taskList := make([]map[string]any, len(tasks))
	for i, t := range tasks {
		entry := map[string]any{
			"id":          t.ID,
			"description": t.Description,
			"status":      t.Status,
			"blocked":     t.Blocked,
			"position":    t.Position,
			"created_at":  t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if t.SleepUntil != nil {
			entry["sleep_until"] = t.SleepUntil.Format("2006-01-02T15:04:05Z07:00")
		}
		if t.ParentTaskID != "" {
			entry["parent_task_id"] = t.ParentTaskID
		}
		if t.Outcome != "" {
			entry["outcome"] = t.Outcome
		}
		taskList[i] = entry
	}

	writeJSON(w, http.StatusOK, map[string]any{"tasks": taskList})
}

func (h *Handlers) getChatHistory(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if strings.EqualFold(l, "all") {
			limit = -1
		} else {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
	}

	messages, err := h.service.GetChatHistory(r.Context(), agentID, limit)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"messages": []any{}})
		return
	}

	result := make([]map[string]any, len(messages))
	for i, m := range messages {
		result[i] = map[string]any{
			"id":         m.ID,
			"role":       m.Role,
			"content":    m.Content,
			"created_at": m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"messages": result})
}

// getRuntimeStatus returns the cached runtime status for an agent.
func (h *Handlers) getRuntimeStatus(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	status := h.docker.ContainerStatus(r.Context(), agentID)
	if status != "running" {
		writeJSON(w, http.StatusOK, map[string]any{
			"type": "runtime_status", "state": "stopped", "smart_mode": false, "model": "",
		})
		return
	}

	session := h.hub.GetSession(agentID)
	if session == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"type": "runtime_status", "state": "unknown", "smart_mode": false, "model": "",
		})
		return
	}

	rs := session.GetRuntimeStatus()
	if rs == nil {
		session.requestRuntimeStatus()
		writeJSON(w, http.StatusOK, map[string]any{
			"type": "runtime_status", "state": "unknown", "smart_mode": false, "model": "",
		})
		return
	}

	writeJSON(w, http.StatusOK, rs)
}
