package main

import (
	"io"
	"net/http"
	"path/filepath"
)

// File upload handler

func (h *Handlers) handleUpload(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	status := h.docker.ContainerStatus(r.Context(), agentID)
	if status != "running" {
		writeError(w, http.StatusBadRequest, "container is not running")
		return
	}

	// 50MB limit
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	// Sanitize filename — use only the base name
	filename := filepath.Base(header.Filename)
	if filename == "." || filename == "/" {
		filename = "upload"
	}

	if err := h.docker.CopyFileToContainer(r.Context(), agentID, filename, data); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to copy file to container: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path": "/data/uploads/" + filename,
		"name": filename,
		"size": len(data),
	})
}
