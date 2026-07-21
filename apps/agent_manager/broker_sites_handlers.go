package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
	"github.com/original-david-knight/go_wild/tools"
)

var siteSlugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

var siteReservedSlugs = map[string]bool{
	"api": true, "admin": true, "static": true, "health": true,
	"help": true, "paywall": true, "sites": true, "www": true,
	"mail": true, "ftp": true, "blog": true, "app": true,
	"accounts": true, "a": true,
}

// handleSitePublish handles POST /broker/v1/sites/publish.
// Copies the directory from the agent's Docker container as a tar archive,
// then uploads it to agent_net via a signed multipart request.
func (h *BrokerSitesHandler) handleSitePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}

	var input tools.PublishSiteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Validate slug
	input.Slug = strings.TrimSpace(input.Slug)
	if !siteSlugRegex.MatchString(input.Slug) {
		writeError(w, http.StatusBadRequest, "slug must be 3-40 characters, lowercase letters, numbers and hyphens only")
		return
	}
	if siteReservedSlugs[input.Slug] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("slug %q is reserved", input.Slug))
		return
	}

	// Validate title
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	// Validate directory
	input.Directory = strings.TrimSpace(input.Directory)
	if input.Directory == "" {
		writeError(w, http.StatusBadRequest, "directory is required")
		return
	}

	// Copy directory from agent's Docker container as tar
	tarData, err := h.copyDirFromContainer(r.Context(), agentID, input.Directory)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read directory from container: "+err.Error())
		return
	}

	// Build a2a client for this agent (derives Ed25519 keys from wallet seed)
	svc := data.NewAgentService(h.db, agentID)
	client, err := newA2AAgentNetClient(r.Context(), svc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent_net client: "+err.Error())
		return
	}

	// Upload to agent_net via signed multipart request
	result, err := client.doMultipartUpload(r.Context(), "/api/v1/sites/publish", "site.tar", tarData, map[string]string{
		"slug":  input.Slug,
		"title": input.Title,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "agent_net upload failed: "+err.Error())
		return
	}

	result["status"] = "success"
	writeJSON(w, http.StatusOK, result)
}

// handleSiteList handles POST /broker/v1/sites/list.
func (h *BrokerSitesHandler) handleSiteList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}

	svc := data.NewAgentService(h.db, agentID)
	client, err := newA2AAgentNetClient(r.Context(), svc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent_net client: "+err.Error())
		return
	}

	result, err := client.doJSON(r.Context(), "GET", "/api/v1/sites/list", nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "agent_net request failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// copyDirFromContainer reads an entire directory from an agent's Docker container as raw tar bytes.
func (h *BrokerSitesHandler) copyDirFromContainer(ctx context.Context, agentID, dirPath string) ([]byte, error) {
	cName := dockermgr.ContainerName(agentID)

	reader, _, err := h.docker.CopyFromContainer(ctx, cName, dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to copy from container: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read tar data: %w", err)
	}

	return data, nil
}
