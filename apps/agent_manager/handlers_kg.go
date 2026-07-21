package main

import (
	"net/http"
	"strings"
)

// Knowledge Graph handlers

type kgActionHandlerFunc func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, nodeID string)

var knowledgeGraphActionHandlers = map[string]kgActionHandlerFunc{
	"": func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, _ string) {
		h.listKGNodes(w, r, agentID)
	},
	"nodes": func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, _ string) {
		h.listKGNodes(w, r, agentID)
	},
	"edges": func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, _ string) {
		h.listKGEdges(w, r, agentID)
	},
	"search": func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, _ string) {
		h.searchKGNodes(w, r, agentID)
	},
	"node": func(h *Handlers, w http.ResponseWriter, r *http.Request, agentID, nodeID string) {
		if nodeID == "" {
			writeError(w, http.StatusBadRequest, "node ID required")
			return
		}
		h.getKGNode(w, r, agentID, nodeID)
	},
}

func isKnowledgeGraphAction(action string) bool {
	_, ok := knowledgeGraphActionHandlers[action]
	return ok
}

func (h *Handlers) handleKnowledgeGraph(w http.ResponseWriter, r *http.Request, agentID string) {
	action, nodeID := parseKnowledgeGraphRoute(r.URL.Path, agentID)
	if !isKnowledgeGraphAction(action) {
		writeError(w, http.StatusNotFound, "unknown kg action: "+action)
		return
	}

	handler, ok := knowledgeGraphActionHandlers[action]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown kg action: "+action)
		return
	}
	handler(h, w, r, agentID, nodeID)
}

func parseKnowledgeGraphRoute(path, agentID string) (action string, nodeID string) {
	prefix := "/api/agents/" + agentID + "/kg"
	subPath := strings.TrimPrefix(path, prefix)
	subPath = strings.TrimPrefix(subPath, "/")

	parts := strings.SplitN(subPath, "/", 2)
	if len(parts) > 0 {
		action = parts[0]
	}
	if len(parts) > 1 {
		nodeID = parts[1]
	}
	return action, nodeID
}

func (h *Handlers) listKGNodes(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	nodeType := r.URL.Query().Get("type")
	nodes, err := h.service.ListKGNodes(r.Context(), agentID, nodeType)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}})
		return
	}

	result := make([]map[string]any, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		result[i] = map[string]any{
			"id":         n.ID,
			"name":       n.Name,
			"type":       n.Type,
			"properties": n.Properties,
			"created_at": n.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			"updated_at": n.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"nodes": result})
}

func (h *Handlers) listKGEdges(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	edges, err := h.service.ListKGEdges(r.Context(), agentID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"edges": []any{}})
		return
	}

	result := make([]map[string]any, len(edges))
	for i := range edges {
		e := &edges[i]
		result[i] = map[string]any{
			"id":             e.ID,
			"source_node_id": e.SourceNodeID,
			"target_node_id": e.TargetNodeID,
			"relation_type":  e.RelationType,
			"properties":     e.Properties,
			"weight":         e.Weight,
			"created_at":     e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"edges": result})
}

func (h *Handlers) getKGNode(w http.ResponseWriter, r *http.Request, agentID, nodeID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	node, neighbors, err := h.service.GetKGNodeWithNeighbors(r.Context(), agentID, nodeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found: "+err.Error())
		return
	}

	neighborList := make([]map[string]any, len(neighbors))
	for i := range neighbors {
		nb := &neighbors[i]
		neighborList[i] = map[string]any{
			"node": map[string]any{
				"id":         nb.Node.ID,
				"name":       nb.Node.Name,
				"type":       nb.Node.Type,
				"properties": nb.Node.Properties,
			},
			"edge": map[string]any{
				"id":            nb.Edge.ID,
				"relation_type": nb.Edge.RelationType,
				"weight":        nb.Edge.Weight,
				"direction":     nb.Direction,
			},
		}
	}

	nodeMap := map[string]any{
		"id":         node.ID,
		"name":       node.Name,
		"type":       node.Type,
		"properties": node.Properties,
		"created_at": node.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"updated_at": node.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if node.Notes != "" {
		nodeMap["notes"] = node.Notes
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"node":      nodeMap,
		"neighbors": neighborList,
	})
}

func (h *Handlers) searchKGNodes(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' required")
		return
	}

	nodes, err := h.service.SearchKGNodes(r.Context(), agentID, query)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}})
		return
	}

	result := make([]map[string]any, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		result[i] = map[string]any{
			"id":         n.ID,
			"name":       n.Name,
			"type":       n.Type,
			"properties": n.Properties,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"nodes": result})
}
