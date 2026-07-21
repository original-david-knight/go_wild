package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/crypto"
)

type chatHistoryInput struct {
	Limit int `json:"limit"`
}

type historySnapshotInput struct {
	Payload string `json:"payload"`
}

type historySummaryInput struct {
	Content string `json:"content"`
}

type dataAccessToolHandler func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, inputJSON []byte) (any, error)

var dataAccessToolHandlers = map[string]dataAccessToolHandler{
	"get_soul_content":                     (*BrokerToolsHandler).dataAccessGetSoulContent,
	"get_memory":                           (*BrokerToolsHandler).dataAccessGetMemory,
	"get_agent_name":                       (*BrokerToolsHandler).dataAccessGetAgentName,
	"get_system_prompt":                    (*BrokerToolsHandler).dataAccessGetSystemPrompt,
	"get_company_context":                  (*BrokerToolsHandler).dataAccessGetCompanyContext,
	"get_company_method_tools":             (*BrokerToolsHandler).dataAccessGetCompanyMethodTools,
	"get_deep_research_method_tools":       (*BrokerToolsHandler).dataAccessGetDeepResearchMethodTools,
	"get_enabled_tools":                    (*BrokerToolsHandler).dataAccessGetEnabledTools,
	"get_wallet_addresses":                 (*BrokerToolsHandler).dataAccessGetWalletAddresses,
	"company_finance_get_wallet_addresses": (*BrokerToolsHandler).dataAccessGetWalletAddresses,
	"get_history_snapshot":                 (*BrokerToolsHandler).dataAccessGetHistorySnapshot,
	"save_history_snapshot":                (*BrokerToolsHandler).dataAccessSaveHistorySnapshot,
	"delete_history_snapshot":              (*BrokerToolsHandler).dataAccessDeleteHistorySnapshot,
	"get_history_summary":                  (*BrokerToolsHandler).dataAccessGetHistorySummary,
	"save_history_summary":                 (*BrokerToolsHandler).dataAccessSaveHistorySummary,
	"delete_history_summary":               (*BrokerToolsHandler).dataAccessDeleteHistorySummary,
	"get_capabilities":                     (*BrokerToolsHandler).dataAccessGetCapabilities,
	"get_chat_history":                     (*BrokerToolsHandler).dataAccessGetChatHistory,
}

func (h *BrokerToolsHandler) callDataAccessTools(ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	handler, ok := dataAccessToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, agentID, svc, inputJSON)
	return true, result, err
}

func (h *BrokerToolsHandler) dataAccessGetSoulContent(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	soul, err := svc.GetSoul(ctx)
	if err != nil {
		return nil, err
	}
	if soul == nil {
		return map[string]any{"content": ""}, nil
	}
	return map[string]any{"content": soul.Content}, nil
}

func (h *BrokerToolsHandler) dataAccessGetMemory(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	entry, err := svc.GetMemory(ctx)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return map[string]any{"content": ""}, nil
	}
	return map[string]any{"content": entry.Content}, nil
}

func (h *BrokerToolsHandler) dataAccessGetAgentName(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	agent, err := svc.GetAgent(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": agent.Name}, nil
}

func (h *BrokerToolsHandler) dataAccessGetSystemPrompt(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	agent, err := svc.GetAgent(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"system_prompt": agent.SystemPrompt}, nil
}

func (h *BrokerToolsHandler) dataAccessGetCompanyContext(ctx context.Context, agentID string, _ *data.AgentService, _ []byte) (any, error) {
	member, err := data.GetCompanyMemberForAgent(ctx, h.db, agentID)
	if err != nil {
		return nil, err
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return map[string]any{
			"has_company": false,
			"company_id":  "",
			"role":        "",
			"is_ceo":      false,
		}, nil
	}

	company, err := data.GetCompany(ctx, h.db, member.CompanyID)
	if err != nil {
		return nil, err
	}
	isCEO := false
	ceoAgentID := ""
	companyName := ""
	companyDescription := ""
	if company != nil {
		ceoAgentID = strings.TrimSpace(company.CEOAgentID)
		companyName = strings.TrimSpace(company.Name)
		companyDescription = strings.TrimSpace(company.Description)
		isCEO = ceoAgentID == strings.TrimSpace(agentID)
	}
	return map[string]any{
		"has_company":         true,
		"company_id":          strings.TrimSpace(member.CompanyID),
		"company_name":        companyName,
		"company_description": companyDescription,
		"role":                strings.TrimSpace(member.Role),
		"is_ceo":              isCEO,
		"ceo_agent_id":        ceoAgentID,
	}, nil
}

func (h *BrokerToolsHandler) dataAccessGetCompanyMethodTools(ctx context.Context, agentID string, _ *data.AgentService, _ []byte) (any, error) {
	specs, err := listCompanyMethodTools(ctx, h.db, agentID)
	if err != nil {
		return nil, err
	}
	tools := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, spec.asMap())
	}
	return map[string]any{"tools": tools}, nil
}

func (h *BrokerToolsHandler) dataAccessGetDeepResearchMethodTools(ctx context.Context, _ string, _ *data.AgentService, _ []byte) (any, error) {
	specs, err := listDeepResearchMethodTools(ctx, h.db)
	if err != nil {
		return nil, err
	}
	tools := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, spec.asMap())
	}
	return map[string]any{"tools": tools}, nil
}

func (h *BrokerToolsHandler) dataAccessGetEnabledTools(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	agent, err := svc.GetAgent(ctx)
	if err != nil {
		return nil, err
	}
	enabled := agent.EnabledTools()
	if enabled == nil {
		// Not configured yet — all tools enabled.
		return map[string]any{"enabled_tools": nil}, nil
	}
	ids := make([]string, 0, len(enabled))
	for id := range enabled {
		ids = append(ids, id)
	}
	return map[string]any{"enabled_tools": ids}, nil
}

func (h *BrokerToolsHandler) dataAccessGetWalletAddresses(ctx context.Context, agentID string, _ *data.AgentService, _ []byte) (any, error) {
	member, err := data.GetCompanyMemberForAgent(ctx, h.db, agentID)
	if err != nil {
		return nil, err
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return nil, fmt.Errorf("wallet tools require company membership")
	}

	companyID := strings.TrimSpace(member.CompanyID)
	seedPhrase, err := data.EnsureCompanyWalletSeedPhrase(ctx, h.db, companyID)
	if err != nil {
		return nil, err
	}
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to derive keys: %w", err)
	}
	return map[string]any{
		"eth_address":    derived.EthAddress,
		"sol_address":    derived.SolAddress,
		"identity_scope": "company",
		"company_id":     companyID,
	}, nil
}

func (h *BrokerToolsHandler) dataAccessGetHistorySnapshot(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	snap, err := svc.GetHistorySnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return map[string]any{"payload": ""}, nil
	}
	return map[string]any{
		"payload":    snap.Payload,
		"updated_at": snap.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

func (h *BrokerToolsHandler) dataAccessSaveHistorySnapshot(ctx context.Context, _ string, svc *data.AgentService, inputJSON []byte) (any, error) {
	return callWithInput[historySnapshotInput](inputJSON, func(input historySnapshotInput) (any, error) {
		if err := svc.SaveHistorySnapshot(ctx, input.Payload); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	})
}

func (h *BrokerToolsHandler) dataAccessDeleteHistorySnapshot(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	if err := svc.DeleteHistorySnapshot(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (h *BrokerToolsHandler) dataAccessGetHistorySummary(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	summary, err := svc.GetHistorySummary(ctx)
	if err != nil {
		return nil, err
	}
	if summary == nil {
		return map[string]any{"content": ""}, nil
	}
	return map[string]any{"content": summary.Content}, nil
}

func (h *BrokerToolsHandler) dataAccessSaveHistorySummary(ctx context.Context, _ string, svc *data.AgentService, inputJSON []byte) (any, error) {
	return callWithInput[historySummaryInput](inputJSON, func(input historySummaryInput) (any, error) {
		if err := svc.SaveHistorySummary(ctx, input.Content); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	})
}

func (h *BrokerToolsHandler) dataAccessDeleteHistorySummary(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	if err := svc.DeleteHistorySummary(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (h *BrokerToolsHandler) dataAccessGetCapabilities(ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
	caps, err := svc.GetCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	methods, err := data.NewAgentService(h.db, "system").ListA2AMethods(ctx)
	if err != nil {
		return nil, err
	}
	methodByName := make(map[string]data.A2AMethod, len(methods))
	for _, m := range methods {
		methodByName[strings.TrimSpace(m.Method)] = m
	}

	capList := make([]map[string]any, len(caps))
	for i, c := range caps {
		desc := ""
		instructions := ""
		var inputSchema any
		var outputSchema any
		if m, ok := methodByName[strings.TrimSpace(c.Method)]; ok {
			desc = strings.TrimSpace(m.Description)
			instructions = strings.TrimSpace(m.Instructions)
			if parsed, err := parseCapabilitySchema(m.InputSchemaJSON); err == nil && parsed != nil {
				inputSchema = parsed
			}
			if parsed, err := parseCapabilitySchema(m.OutputSchemaJSON); err == nil && parsed != nil {
				outputSchema = parsed
			}
		}
		item := map[string]any{
			"role":         c.Role,
			"method":       c.Method,
			"description":  desc,
			"instructions": instructions,
		}
		if inputSchema != nil {
			item["input_schema"] = inputSchema
		}
		if outputSchema != nil {
			item["output_schema"] = outputSchema
		}
		capList[i] = item
	}
	return map[string]any{"capabilities": capList}, nil
}

func (h *BrokerToolsHandler) dataAccessGetChatHistory(ctx context.Context, _ string, svc *data.AgentService, inputJSON []byte) (any, error) {
	return callWithInput[chatHistoryInput](inputJSON, func(input chatHistoryInput) (any, error) {
		limit := input.Limit
		if limit == 0 {
			limit = 50
		}
		messages, err := svc.GetChatHistory(ctx, limit)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, len(messages))
		for i, m := range messages {
			out[i] = map[string]any{
				"id":         m.ID,
				"role":       m.Role,
				"content":    m.Content,
				"created_at": m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			}
		}
		return map[string]any{"messages": out}, nil
	})
}
