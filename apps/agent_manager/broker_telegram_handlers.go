package main

import (
	"encoding/json"
	"net/http"

	"github.com/original-david-knight/go_wild/tools"
)

func (h *BrokerTelegramHandler) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	agentID := BrokerAgentID(r.Context())
	t, err := h.getOrCreateTelegram(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var input tools.TelegramSendInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	result, err := t.TelegramSendTool(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result.ToMap())
}

func (h *BrokerTelegramHandler) handleGetUpdates(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	t, err := h.getOrCreateTelegram(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	input := tools.TelegramGetUpdatesInput{
		Clear: r.URL.Query().Get("clear") == "true",
	}

	result, err := t.TelegramGetUpdatesTool(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result.ToMap())
}

func (h *BrokerTelegramHandler) handleGetChats(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	t, err := h.getOrCreateTelegram(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result, err := t.TelegramGetChatsTool(r.Context(), tools.TelegramGetChatsInput{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result.ToMap())
}

func (h *BrokerTelegramHandler) handleBotInfo(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	t, err := h.getOrCreateTelegram(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result, err := t.TelegramGetBotInfoTool(r.Context(), tools.TelegramGetBotInfoInput{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result.ToMap())
}
