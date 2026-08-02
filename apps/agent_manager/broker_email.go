package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
	"log"
	"net/http"
)

// BrokerEmailHandler handles email proxy requests.
type BrokerEmailHandler struct {
	service *AgentService
}

// NewBrokerEmailHandler creates a new email broker handler.
func NewBrokerEmailHandler(service *AgentService) *BrokerEmailHandler {
	return &BrokerEmailHandler{service: service}
}

// getEmailTools creates an EmailTools instance for the given agent.
func (h *BrokerEmailHandler) getEmailTools(ctx context.Context, agentID string) (*tools.EmailTools, error) {
	agent, err := h.service.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if agent.AgentMailAPIKey == "" {
		return nil, fmt.Errorf("agent has no AgentMail API key configured")
	}
	if agent.AgentMailInboxID == "" {
		return nil, fmt.Errorf("agent has no AgentMail inbox ID configured")
	}

	return tools.NewEmailTools(agent.AgentMailAPIKey, agent.AgentMailInboxID), nil
}

// getEmailOutbox creates an EmailOutbox instance for the given agent.
func (h *BrokerEmailHandler) getEmailOutbox(ctx context.Context, agentID string) (*tools.EmailOutbox, error) {
	emailTools, err := h.getEmailTools(ctx, agentID)
	if err != nil {
		return nil, err
	}
	agentSvc := data.NewAgentService(h.service.db, agentID)
	return tools.NewEmailOutbox(emailTools, agentSvc), nil
}

func (h *BrokerEmailHandler) handleList(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	emailTools, err := h.getEmailTools(r.Context(), agentID)
	if err != nil {
		log.Printf("Broker email error for agent %s: %v", agentID, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var input tools.ListEmailsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	result, err := emailTools.ListEmailsTool(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result.ToMap())
}

func (h *BrokerEmailHandler) handleRead(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	emailTools, err := h.getEmailTools(r.Context(), agentID)
	if err != nil {
		log.Printf("Broker email error for agent %s: %v", agentID, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var input tools.ReadEmailInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	result, err := emailTools.ReadEmailTool(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result.ToMap())
}

func (h *BrokerEmailHandler) handleSend(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	outbox, err := h.getEmailOutbox(r.Context(), agentID)
	if err != nil {
		log.Printf("Broker email error for agent %s: %v", agentID, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var input tools.SendEmailInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	result, err := outbox.SendEmailTool(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result.ToMap())
}
