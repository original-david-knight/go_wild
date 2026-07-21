package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
	"github.com/original-david-knight/go_wild/tools"
)

// handlePaywallCreate handles POST /broker/v1/paywall/create.
// Copies the file from the agent's Docker container, then uploads it to agent_net
// via a signed multipart request to create the product on the public server.
func (h *BrokerPaywallHandler) handlePaywallCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}

	var input tools.CreateCryptoPaywallInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Validate input
	if input.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if input.FilePath == "" {
		writeError(w, http.StatusBadRequest, "file_path is required")
		return
	}
	if input.PriceUSDC == "" {
		writeError(w, http.StatusBadRequest, "price_usdc is required")
		return
	}
	price, err := strconv.ParseFloat(input.PriceUSDC, 64)
	if err != nil || price <= 0 {
		writeError(w, http.StatusBadRequest, "price_usdc must be a positive number")
		return
	}
	if input.Chain != "polygon" && input.Chain != "solana" {
		writeError(w, http.StatusBadRequest, "chain must be 'polygon' or 'solana'")
		return
	}
	if input.WalletAddress == "" {
		writeError(w, http.StatusBadRequest, "wallet_address is required")
		return
	}

	// Copy file from agent's Docker container
	fileName := filepath.Base(input.FilePath)
	fileData, err := h.copyFileFromContainer(r.Context(), agentID, input.FilePath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file from container: "+err.Error())
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
	result, err := client.doMultipartUpload(r.Context(), "/api/v1/paywall/create", fileName, fileData, map[string]string{
		"title":          input.Title,
		"description":    input.Description,
		"price_usdc":     input.PriceUSDC,
		"chain":          input.Chain,
		"wallet_address": input.WalletAddress,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "agent_net upload failed: "+err.Error())
		return
	}

	// Add status field for backward compat with agent tool
	result["status"] = "success"

	writeJSON(w, http.StatusOK, result)
}

// copyFileFromContainer reads a file from an agent's Docker container.
func (h *BrokerPaywallHandler) copyFileFromContainer(ctx context.Context, agentID, filePath string) ([]byte, error) {
	cName := dockermgr.ContainerName(agentID)

	reader, _, err := h.docker.CopyFromContainer(ctx, cName, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to copy from container: %w", err)
	}
	defer reader.Close()

	// CopyFromContainer returns a tar archive
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar: %w", err)
		}
		if header.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("failed to read file data: %w", err)
			}
			return data, nil
		}
	}

	return nil, fmt.Errorf("file not found in container: %s", filePath)
}

// doMultipartUpload sends a signed multipart/form-data POST to agent_net.
func (c *a2aAgentNetClient) doMultipartUpload(ctx context.Context, path, fileName string, fileData []byte, fields map[string]string) (map[string]any, error) {
	// Build multipart body
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, fmt.Errorf("failed to write field %s: %w", k, err)
		}
	}

	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to create file part: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return nil, fmt.Errorf("failed to write file data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize multipart: %w", err)
	}

	bodyBytes := buf.Bytes()

	// Sign the raw multipart body (same as JSON — SignRequest hashes body bytes)
	timestamp, signature := c.sign("POST", path, bodyBytes)

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Agent-ID", c.agentID)
	req.Header.Set("X-Agent-Timestamp", timestamp)
	req.Header.Set("X-Agent-Sig", signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent_net request failed: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent_net response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respData, &result); err != nil {
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("agent_net error (%d): %s", resp.StatusCode, string(respData))
		}
		return nil, fmt.Errorf("unexpected non-JSON agent_net response")
	}

	if resp.StatusCode >= 400 {
		msg, _ := result["message"].(string)
		if msg == "" {
			msg, _ = result["error"].(string)
		}
		if msg == "" {
			msg = string(respData)
		}
		return nil, fmt.Errorf("agent_net error (%d): %s", resp.StatusCode, msg)
	}

	return result, nil
}
