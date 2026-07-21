package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools/amazon"
)

type amazonToolHandlerFunc func(ctx context.Context, client *amazon.PAAClient, inputJSON []byte) (any, error)

var amazonToolHandlers = map[string]amazonToolHandlerFunc{
	"amazon_search": func(ctx context.Context, client *amazon.PAAClient, inputJSON []byte) (any, error) {
		return callWithInput[amazon.SearchInput](inputJSON, func(input amazon.SearchInput) (any, error) {
			tools := amazon.NewAmazonTools(client)
			r, err := tools.AmazonSearchTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"amazon_get_product": func(ctx context.Context, client *amazon.PAAClient, inputJSON []byte) (any, error) {
		return callWithInput[amazon.GetProductInput](inputJSON, func(input amazon.GetProductInput) (any, error) {
			tools := amazon.NewAmazonTools(client)
			r, err := tools.AmazonGetProductTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
}

func (h *BrokerToolsHandler) callAmazonTools(ctx context.Context, agentID, toolName string, inputJSON []byte) (bool, any, error) {
	if !isAmazonTool(toolName) {
		return false, nil, nil
	}

	client, companyID, err := h.companyAmazonClientForAgent(ctx, agentID)
	if err != nil {
		return true, nil, err
	}

	handler, ok := amazonToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, callErr := handler(ctx, client, inputJSON)
	if callErr != nil {
		return true, nil, callErr
	}
	return true, annotateAmazonResult(result, companyID), nil
}

func isAmazonTool(toolName string) bool {
	_, ok := amazonToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) companyAmazonClientForAgent(ctx context.Context, agentID string) (*amazon.PAAClient, string, error) {
	member, err := data.GetCompanyMemberForAgent(ctx, h.db, agentID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve company membership: %w", err)
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return nil, "", fmt.Errorf("amazon tools require company membership")
	}
	client, err := h.companyAmazonClient(ctx, member.CompanyID)
	if err != nil {
		return nil, "", err
	}
	return client, member.CompanyID, nil
}

func (h *BrokerToolsHandler) companyAmazonClient(ctx context.Context, companyID string) (*amazon.PAAClient, error) {
	conn, err := data.GetCompanyAmazonConnection(ctx, h.db, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to load company amazon connection: %w", err)
	}
	if conn == nil || !conn.Enabled {
		return nil, fmt.Errorf("company amazon connection is missing or disabled")
	}
	accessKey := strings.TrimSpace(conn.AccessKeyEnc)
	secretKey := strings.TrimSpace(conn.SecretKeyEnc)
	partnerTag := strings.TrimSpace(conn.PartnerTag)
	if accessKey == "" || secretKey == "" || partnerTag == "" {
		return nil, fmt.Errorf("company amazon connection is incomplete")
	}
	return amazon.NewPAAClient(accessKey, secretKey, partnerTag, conn.Marketplace), nil
}

func annotateAmazonResult(result any, companyID string) any {
	if companyID == "" {
		return result
	}
	if payload, ok := result.(map[string]any); ok {
		payload["identity_scope"] = "company"
		payload["company_id"] = companyID
		return payload
	}
	return map[string]any{
		"result":         result,
		"identity_scope": "company",
		"company_id":     companyID,
	}
}
