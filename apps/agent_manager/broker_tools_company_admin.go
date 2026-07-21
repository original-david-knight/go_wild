package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
)

type companyAdminToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, agentID string, company *data.Company, member *data.CompanyMember, isCEO bool, inputJSON []byte) (any, error)

var companyAdminToolHandlers = map[string]companyAdminToolHandlerFunc{
	"company_admin_get_context": func(_ *BrokerToolsHandler, _ context.Context, _ string, company *data.Company, member *data.CompanyMember, isCEO bool, _ []byte) (any, error) {
		return companyAdminContextMap(company, member, isCEO), nil
	},
	"company_admin_list_members": func(h *BrokerToolsHandler, ctx context.Context, _ string, company *data.Company, member *data.CompanyMember, isCEO bool, _ []byte) (any, error) {
		members, err := data.ListCompanyMembers(ctx, h.db, company.ID)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, len(members))
		for i, m := range members {
			items[i] = companyMemberToResponse(m)
		}
		return map[string]any{
			"company":        companyToResponse(company),
			"members":        items,
			"membership":     companyMemberToResponse(*member),
			"is_ceo":         isCEO,
			"identity_scope": "company",
		}, nil
	},
	"company_admin_update_company": func(h *BrokerToolsHandler, ctx context.Context, _ string, company *data.Company, member *data.CompanyMember, isCEO bool, inputJSON []byte) (any, error) {
		if err := requireCompanyAdminCEO(isCEO); err != nil {
			return nil, err
		}
		return callWithInput[tools.CompanyAdminUpdateCompanyInput](inputJSON, func(input tools.CompanyAdminUpdateCompanyInput) (any, error) {
			if input.Name != nil {
				company.Name = strings.TrimSpace(*input.Name)
			}
			if input.Description != nil {
				company.Description = strings.TrimSpace(*input.Description)
			}
			if err := data.UpdateCompany(ctx, h.db, company); err != nil {
				return nil, err
			}
			out := companyAdminContextMap(company, member, isCEO)
			out["status"] = "updated"
			return out, nil
		})
	},
	"company_admin_add_member": func(h *BrokerToolsHandler, ctx context.Context, _ string, company *data.Company, _ *data.CompanyMember, isCEO bool, inputJSON []byte) (any, error) {
		if err := requireCompanyAdminCEO(isCEO); err != nil {
			return nil, err
		}
		return callWithInput[tools.CompanyAdminAddMemberInput](inputJSON, func(input tools.CompanyAdminAddMemberInput) (any, error) {
			if err := data.AddAgentToCompany(ctx, h.db, company.ID, input.AgentID, input.Role); err != nil {
				return nil, err
			}
			return map[string]any{
				"status":         "added",
				"agent_id":       strings.TrimSpace(input.AgentID),
				"company_id":     company.ID,
				"identity_scope": "company",
			}, nil
		})
	},
	"company_admin_remove_member": func(h *BrokerToolsHandler, ctx context.Context, _ string, company *data.Company, _ *data.CompanyMember, isCEO bool, inputJSON []byte) (any, error) {
		if err := requireCompanyAdminCEO(isCEO); err != nil {
			return nil, err
		}
		return callWithInput[tools.CompanyAdminRemoveMemberInput](inputJSON, func(input tools.CompanyAdminRemoveMemberInput) (any, error) {
			if err := data.RemoveAgentFromCompany(ctx, h.db, company.ID, input.AgentID); err != nil {
				return nil, err
			}
			return map[string]any{
				"status":         "removed",
				"agent_id":       strings.TrimSpace(input.AgentID),
				"company_id":     company.ID,
				"identity_scope": "company",
			}, nil
		})
	},
	"company_admin_set_ceo": func(h *BrokerToolsHandler, ctx context.Context, agentID string, company *data.Company, member *data.CompanyMember, isCEO bool, inputJSON []byte) (any, error) {
		if err := requireCompanyAdminCEO(isCEO); err != nil {
			return nil, err
		}
		return callWithInput[tools.CompanyAdminSetCEOInput](inputJSON, func(input tools.CompanyAdminSetCEOInput) (any, error) {
			if err := data.SetCompanyCEO(ctx, h.db, company.ID, input.AgentID); err != nil {
				return nil, err
			}
			updated, err := data.GetCompany(ctx, h.db, company.ID)
			if err != nil {
				return nil, err
			}
			if updated != nil {
				company = updated
			}
			out := companyAdminContextMap(company, member, strings.TrimSpace(company.CEOAgentID) == strings.TrimSpace(agentID))
			out["status"] = "ceo_updated"
			return out, nil
		})
	},
	"company_admin_send_heartbeat": func(h *BrokerToolsHandler, ctx context.Context, agentID string, company *data.Company, _ *data.CompanyMember, isCEO bool, inputJSON []byte) (any, error) {
		if err := requireCompanyAdminCEO(isCEO); err != nil {
			return nil, err
		}
		return callWithInput[tools.CompanyAdminSendHeartbeatInput](inputJSON, func(input tools.CompanyAdminSendHeartbeatInput) (any, error) {
			return h.sendCompanyHeartbeatForMembers(ctx, company, agentID, isCEO, input.Message, input.IncludeCEO, input.MemberFilter)
		})
	},
	"send_company_heartbeat": func(h *BrokerToolsHandler, ctx context.Context, agentID string, company *data.Company, _ *data.CompanyMember, isCEO bool, inputJSON []byte) (any, error) {
		if err := requireCompanyAdminCEO(isCEO); err != nil {
			return nil, err
		}
		return callWithInput[tools.SendCompanyHeartbeatInput](inputJSON, func(input tools.SendCompanyHeartbeatInput) (any, error) {
			targetCompanyID := strings.TrimSpace(input.CompanyID)
			if targetCompanyID == "" {
				targetCompanyID = company.ID
			}
			if targetCompanyID != company.ID {
				return nil, fmt.Errorf("company_id must match caller company")
			}
			return h.sendCompanyHeartbeatForMembers(ctx, company, agentID, isCEO, input.Message, input.IncludeCEO, input.MemberFilter)
		})
	},
}

func isCompanyAdminTool(toolName string) bool {
	_, ok := companyAdminToolHandlers[toolName]
	return ok
}

func requireCompanyAdminCEO(isCEO bool) error {
	if isCEO {
		return nil
	}
	return fmt.Errorf("company admin mutation requires ceo role")
}

func (h *BrokerToolsHandler) callCompanyAdminTools(ctx context.Context, agentID string, toolName string, inputJSON []byte) (bool, any, error) {
	if !isCompanyAdminTool(toolName) {
		return false, nil, nil
	}

	company, member, isCEO, err := h.resolveCompanyContext(ctx, agentID)
	if err != nil {
		return true, nil, err
	}
	if company == nil || member == nil {
		return true, nil, fmt.Errorf("company admin tools require company membership")
	}

	handler, ok := companyAdminToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, callErr := handler(h, ctx, agentID, company, member, isCEO, inputJSON)
	return true, result, callErr
}

func (h *BrokerToolsHandler) resolveCompanyContext(ctx context.Context, agentID string) (*data.Company, *data.CompanyMember, bool, error) {
	member, err := data.GetCompanyMemberForAgent(ctx, h.db, agentID)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to resolve company membership: %w", err)
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return nil, nil, false, nil
	}
	company, err := data.GetCompany(ctx, h.db, member.CompanyID)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load company: %w", err)
	}
	isCEO := strings.TrimSpace(company.CEOAgentID) == strings.TrimSpace(agentID)
	return company, member, isCEO, nil
}

func companyAdminContextMap(company *data.Company, member *data.CompanyMember, isCEO bool) map[string]any {
	if company == nil {
		return map[string]any{
			"company":        nil,
			"membership":     nil,
			"is_ceo":         false,
			"identity_scope": "company",
		}
	}
	out := map[string]any{
		"company":        companyToResponse(company),
		"is_ceo":         isCEO,
		"identity_scope": "company",
		"company_id":     company.ID,
	}
	if member != nil {
		out["membership"] = companyMemberToResponse(*member)
	}
	return out
}

func (h *BrokerToolsHandler) sendCompanyHeartbeat(agentID, message string) error {
	if h.sendHeartbeatFn != nil {
		return h.sendHeartbeatFn(agentID, message)
	}
	if h.workerManager == nil {
		return fmt.Errorf("worker manager not configured")
	}
	return h.workerManager.SendHeartbeat(agentID, message)
}

func (h *BrokerToolsHandler) sendCompanyHeartbeatForMembers(
	ctx context.Context,
	company *data.Company,
	requestedBy string,
	requestedAsCEO bool,
	message string,
	includeCEOInput *bool,
	memberFilter string,
) (map[string]any, error) {
	if h.sendHeartbeatFn == nil && h.workerManager == nil {
		return nil, fmt.Errorf("worker manager not configured")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, fmt.Errorf("message is required")
	}

	includeCEO := true
	if includeCEOInput != nil {
		includeCEO = *includeCEOInput
	}
	memberFilter = strings.TrimSpace(memberFilter)

	members, err := data.ListCompanyMembers(ctx, h.db, company.ID)
	if err != nil {
		return nil, err
	}

	sent := make([]string, 0, len(members))
	failed := make(map[string]string)
	for _, m := range members {
		targetAgentID := strings.TrimSpace(m.AgentID)
		if targetAgentID == "" {
			continue
		}
		if !includeCEO && targetAgentID == strings.TrimSpace(company.CEOAgentID) {
			continue
		}
		if memberFilter != "" && !strings.EqualFold(strings.TrimSpace(m.Role), memberFilter) {
			continue
		}
		if err := h.sendCompanyHeartbeat(targetAgentID, message); err != nil {
			failed[targetAgentID] = err.Error()
			continue
		}
		sent = append(sent, targetAgentID)
	}

	return map[string]any{
		"status":           "sent",
		"company_id":       company.ID,
		"identity_scope":   "company",
		"include_ceo":      includeCEO,
		"member_filter":    memberFilter,
		"sent_agent_ids":   sent,
		"sent_count":       len(sent),
		"failed":           failed,
		"failed_count":     len(failed),
		"requested_by":     requestedBy,
		"requested_as_ceo": requestedAsCEO,
	}, nil
}
