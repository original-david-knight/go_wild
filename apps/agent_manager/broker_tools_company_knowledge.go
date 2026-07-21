package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
)

type companyKnowledgeToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, agentID, companyID string, member *data.CompanyMember, inputJSON []byte) (any, error)

var companyKnowledgeToolHandlers = map[string]companyKnowledgeToolHandlerFunc{
	"company_knowledge_search": func(h *BrokerToolsHandler, ctx context.Context, _ string, companyID string, member *data.CompanyMember, inputJSON []byte) (any, error) {
		return callWithInput[tools.CompanyKnowledgeSearchInput](inputJSON, func(input tools.CompanyKnowledgeSearchInput) (any, error) {
			entries, err := data.ListCompanyKnowledgeEntries(ctx, h.db, companyID, input.Query, input.Kind, input.Limit)
			if err != nil {
				return nil, err
			}
			items := make([]map[string]any, len(entries))
			for i := range entries {
				items[i] = companyKnowledgeEntryMap(&entries[i])
			}
			return map[string]any{
				"entries":         items,
				"company_id":      companyID,
				"identity_scope":  "company",
				"membership_role": member.Role,
			}, nil
		})
	},
	"company_knowledge_add": func(h *BrokerToolsHandler, ctx context.Context, agentID, companyID string, _ *data.CompanyMember, inputJSON []byte) (any, error) {
		return callWithInput[tools.CompanyKnowledgeAddInput](inputJSON, func(input tools.CompanyKnowledgeAddInput) (any, error) {
			entry, err := data.AddCompanyKnowledgeEntry(ctx, h.db, companyID, agentID, input.Kind, input.Title, input.Content, input.Tags, input.Metadata)
			if err != nil {
				return nil, err
			}
			out := companyKnowledgeEntryMap(entry)
			out["company_id"] = companyID
			out["identity_scope"] = "company"
			return out, nil
		})
	},
	"company_knowledge_get": func(h *BrokerToolsHandler, ctx context.Context, _ string, companyID string, _ *data.CompanyMember, inputJSON []byte) (any, error) {
		return callWithInput[tools.CompanyKnowledgeGetInput](inputJSON, func(input tools.CompanyKnowledgeGetInput) (any, error) {
			entry, err := data.GetCompanyKnowledgeEntry(ctx, h.db, companyID, input.EntryID)
			if err != nil {
				return nil, err
			}
			out := companyKnowledgeEntryMap(entry)
			out["company_id"] = companyID
			out["identity_scope"] = "company"
			return out, nil
		})
	},
	"company_knowledge_update": func(h *BrokerToolsHandler, ctx context.Context, _ string, companyID string, _ *data.CompanyMember, inputJSON []byte) (any, error) {
		return callWithInput[tools.CompanyKnowledgeUpdateInput](inputJSON, func(input tools.CompanyKnowledgeUpdateInput) (any, error) {
			entry, err := data.UpdateCompanyKnowledgeEntry(ctx, h.db, companyID, input.EntryID, input.Kind, input.Title, input.Content, input.Tags, input.Metadata)
			if err != nil {
				return nil, err
			}
			out := companyKnowledgeEntryMap(entry)
			out["company_id"] = companyID
			out["identity_scope"] = "company"
			return out, nil
		})
	},
	"company_knowledge_delete": func(h *BrokerToolsHandler, ctx context.Context, _ string, companyID string, _ *data.CompanyMember, inputJSON []byte) (any, error) {
		return callWithInput[tools.CompanyKnowledgeDeleteInput](inputJSON, func(input tools.CompanyKnowledgeDeleteInput) (any, error) {
			if err := data.DeleteCompanyKnowledgeEntry(ctx, h.db, companyID, input.EntryID); err != nil {
				return nil, err
			}
			return map[string]any{
				"status":         "deleted",
				"entry_id":       input.EntryID,
				"company_id":     companyID,
				"identity_scope": "company",
			}, nil
		})
	},
}

func isCompanyKnowledgeTool(toolName string) bool {
	_, ok := companyKnowledgeToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callCompanyKnowledgeTools(ctx context.Context, agentID string, toolName string, inputJSON []byte) (bool, any, error) {
	if !isCompanyKnowledgeTool(toolName) {
		return false, nil, nil
	}

	member, err := data.GetCompanyMemberForAgent(ctx, h.db, agentID)
	if err != nil {
		return true, nil, fmt.Errorf("failed to resolve company membership: %w", err)
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return true, nil, fmt.Errorf("company knowledge tools require company membership")
	}
	companyID := strings.TrimSpace(member.CompanyID)

	handler, ok := companyKnowledgeToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, callErr := handler(h, ctx, agentID, companyID, member, inputJSON)
	return true, result, callErr
}

func companyKnowledgeEntryMap(entry *data.CompanyKnowledgeEntry) map[string]any {
	if entry == nil {
		return map[string]any{}
	}
	var tags []string
	if strings.TrimSpace(entry.TagsJSON) != "" {
		_ = json.Unmarshal([]byte(entry.TagsJSON), &tags)
	}
	metadata := map[string]any{}
	if strings.TrimSpace(entry.MetadataJSON) != "" {
		_ = json.Unmarshal([]byte(entry.MetadataJSON), &metadata)
	}
	return map[string]any{
		"id":                  entry.ID,
		"kind":                entry.Kind,
		"title":               entry.Title,
		"content":             entry.Content,
		"tags":                tags,
		"metadata":            metadata,
		"created_by_agent_id": entry.CreatedByAgentID,
		"created_at":          entry.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"updated_at":          entry.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
