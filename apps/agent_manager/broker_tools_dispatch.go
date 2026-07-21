package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

type brokerToolDispatchFunc func(
	h *BrokerToolsHandler,
	ctx context.Context,
	agentID string,
	svc *data.AgentService,
	toolName string,
	inputJSON []byte,
) (bool, any, error)

var brokerToolDispatchers = []brokerToolDispatchFunc{
	func(h *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callSoulTools(ctx, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callReportTools(ctx, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callWebReaderTools(ctx, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callSkillTools(ctx, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callTaskTools(ctx, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callKnowledgeGraphTools(ctx, agentID, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callCompanyAdminTools(ctx, agentID, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callCompanyKnowledgeTools(ctx, agentID, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callDataAccessTools(ctx, agentID, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callRecurringTools(ctx, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callA2ATools(ctx, agentID, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callMessagingTools(ctx, agentID, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callMCPTools(ctx, agentID, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callCacheTools(ctx, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callCompressTools(ctx, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callClaudeCodeTools(ctx, agentID, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callEcommerceTools(ctx, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callInventorySyncTools(ctx, agentID, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callShopifyTools(ctx, agentID, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callAmazonTools(ctx, agentID, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callSupplierTools(ctx, agentID, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callAdsTools(ctx, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callCompanyMethodTools(ctx, agentID, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callDeepResearchMethodTools(ctx, agentID, svc, toolName, inputJSON)
	},
	func(h *BrokerToolsHandler, ctx context.Context, _ string, _ *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
		return h.callReutersTools(ctx, toolName, inputJSON)
	},
}

func (h *BrokerToolsHandler) callTool(ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (any, error) {
	allowed, err := h.isToolCallAllowed(ctx, agentID, svc, toolName)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, fmt.Errorf("tool %q is disabled for this agent", toolName)
	}

	var spendCost float64
	var spendCategory string
	spendEntityID := agentID

	// Check spend limits before execution
	if h.spendGovernor != nil {
		if cost, category := h.spendGovernor.EstimateCost(toolName, inputJSON); cost > 0 {
			if category == "shopify" {
				member, err := data.GetCompanyMemberForAgent(ctx, h.db, agentID)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve company membership: %w", err)
				}
				if member == nil {
					return nil, fmt.Errorf("shopify tools require company membership")
				}
				spendEntityID = "company:" + strings.TrimSpace(member.CompanyID)
			}
			if err := h.spendGovernor.CheckBudget(ctx, spendEntityID, category, cost); err != nil {
				return nil, err
			}
			spendCost = cost
			spendCategory = category
		}
	}

	result, err := h.dispatchTool(ctx, agentID, svc, toolName, inputJSON)
	if err == nil && h.spendGovernor != nil && spendCost > 0 {
		h.spendGovernor.RecordSpend(ctx, spendEntityID, spendCategory, spendCost, toolName)
	}
	return result, err
}

func (h *BrokerToolsHandler) dispatchTool(ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (any, error) {
	for _, dispatch := range brokerToolDispatchers {
		if handled, result, err := dispatch(h, ctx, agentID, svc, toolName, inputJSON); handled {
			return result, err
		}
	}
	return nil, fmt.Errorf("unknown tool: %s", toolName)
}

// callWithInput unmarshals JSON into the input type and calls the handler.
func callWithInput[T any](inputJSON []byte, fn func(T) (any, error)) (any, error) {
	var input T
	if len(inputJSON) > 0 {
		if err := json.Unmarshal(inputJSON, &input); err != nil {
			return nil, fmt.Errorf("failed to unmarshal input: %w", err)
		}
	}
	return fn(input)
}

// toolResultContent extracts the content from a ToolResult for JSON serialization.
// On success, returns the Content field. On tool error, returns a Go error
// so the broker responds with HTTP 500 and the agent-side creates an ErrorResult.
func toolResultContent(r *loop.ToolResult, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if !r.Success {
		return nil, fmt.Errorf("%s", r.Error)
	}
	return r.Content, nil
}
