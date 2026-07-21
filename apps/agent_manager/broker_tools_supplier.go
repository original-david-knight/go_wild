package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools/supplier"
	"github.com/original-david-knight/go_wild/tools/supplier/providers"
)

type supplierToolHandlerFunc func(ctx context.Context, client supplier.Supplier, inputJSON []byte) (any, error)
type supplierEnvClientFactory func() (supplier.Supplier, error)

var supplierToolHandlers = map[string]supplierToolHandlerFunc{
	"supplier_search_products": func(ctx context.Context, client supplier.Supplier, inputJSON []byte) (any, error) {
		tools := supplier.NewSupplierProductTools(client)
		return callWithInput[supplier.SearchProductsInput](inputJSON, func(input supplier.SearchProductsInput) (any, error) {
			r, err := tools.SupplierSearchProductsTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"supplier_get_product": func(ctx context.Context, client supplier.Supplier, inputJSON []byte) (any, error) {
		tools := supplier.NewSupplierProductTools(client)
		return callWithInput[supplier.GetProductInput](inputJSON, func(input supplier.GetProductInput) (any, error) {
			r, err := tools.SupplierGetProductTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"supplier_get_shipping": func(ctx context.Context, client supplier.Supplier, inputJSON []byte) (any, error) {
		tools := supplier.NewSupplierProductTools(client)
		return callWithInput[supplier.GetShippingInput](inputJSON, func(input supplier.GetShippingInput) (any, error) {
			r, err := tools.SupplierGetShippingTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"supplier_place_order": func(ctx context.Context, client supplier.Supplier, inputJSON []byte) (any, error) {
		tools := supplier.NewSupplierOrderTools(client)
		return callWithInput[supplier.PlaceOrderInput](inputJSON, func(input supplier.PlaceOrderInput) (any, error) {
			r, err := tools.SupplierPlaceOrderTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"supplier_get_order": func(ctx context.Context, client supplier.Supplier, inputJSON []byte) (any, error) {
		tools := supplier.NewSupplierOrderTools(client)
		return callWithInput[supplier.GetOrderInput](inputJSON, func(input supplier.GetOrderInput) (any, error) {
			r, err := tools.SupplierGetOrderTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"supplier_cancel_order": func(ctx context.Context, client supplier.Supplier, inputJSON []byte) (any, error) {
		tools := supplier.NewSupplierOrderTools(client)
		return callWithInput[supplier.CancelOrderInput](inputJSON, func(input supplier.CancelOrderInput) (any, error) {
			r, err := tools.SupplierCancelOrderTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"supplier_get_tracking": func(ctx context.Context, client supplier.Supplier, inputJSON []byte) (any, error) {
		tools := supplier.NewSupplierTrackingTools(client)
		return callWithInput[supplier.GetTrackingInput](inputJSON, func(input supplier.GetTrackingInput) (any, error) {
			r, err := tools.SupplierGetTrackingTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
}

var supplierEnvClientFactories = map[string]supplierEnvClientFactory{
	"cjdropshipping": supplierClientFromCJDropshippingEnv,
	"cj":             supplierClientFromCJDropshippingEnv,
	"topdawg":        supplierClientFromTopDawgEnv,
}

// getSupplierClientFromEnv returns a Supplier implementation based on manager env.
// The provider is determined by the SUPPLIER_DEFAULT_PROVIDER env var (default: topdawg).
func getSupplierClientFromEnv() (supplier.Supplier, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("SUPPLIER_DEFAULT_PROVIDER")))
	if provider == "" {
		provider = "topdawg"
	}

	factory, ok := supplierEnvClientFactories[provider]
	if !ok {
		return nil, fmt.Errorf("unknown supplier provider: %s", provider)
	}
	return factory()
}

func supplierClientFromCJDropshippingEnv() (supplier.Supplier, error) {
	accessToken := strings.TrimSpace(os.Getenv("CJDROPSHIPPING_ACCESS_TOKEN"))
	if accessToken == "" {
		return nil, fmt.Errorf("CJDROPSHIPPING_ACCESS_TOKEN not set")
	}
	platformToken := strings.TrimSpace(os.Getenv("CJDROPSHIPPING_PLATFORM_TOKEN"))
	defaultFromCountry := strings.TrimSpace(os.Getenv("CJDROPSHIPPING_DEFAULT_FROM_COUNTRY"))
	return providers.NewCJDropshipping(accessToken, platformToken, defaultFromCountry), nil
}

func supplierClientFromTopDawgEnv() (supplier.Supplier, error) {
	apiKey := os.Getenv("TOPDAWG_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("TOPDAWG_API_KEY not set")
	}
	supplierID := os.Getenv("TOPDAWG_SUPPLIER_ID")
	if supplierID == "" {
		return nil, fmt.Errorf("TOPDAWG_SUPPLIER_ID not set")
	}
	return providers.NewTopDawg(apiKey, supplierID), nil
}

func (h *BrokerToolsHandler) supplierClientForAgent(ctx context.Context, agentID string) (supplier.Supplier, string, error) {
	member, err := data.GetCompanyMemberForAgent(ctx, h.db, agentID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve company membership: %w", err)
	}
	companyID := ""
	if member != nil {
		companyID = strings.TrimSpace(member.CompanyID)
	}
	if companyID != "" {
		cjConn, err := data.GetCompanyCJDropshippingConnection(ctx, h.db, companyID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load company cjdropshipping connection: %w", err)
		}
		if cjConn != nil && cjConn.Enabled {
			accessToken, err := resolveCompanyCJDropshippingAccessToken(ctx, h.db, cjConn)
			if err != nil {
				return nil, "", fmt.Errorf("failed to resolve company cjdropshipping access token: %w", err)
			}
			return providers.NewCJDropshipping(accessToken, cjConn.PlatformTokenEnc, cjConn.DefaultFromCountryCode), companyID, nil
		}

		conn, err := data.GetCompanyTopDawgConnection(ctx, h.db, companyID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load company topdawg connection: %w", err)
		}
		if conn != nil && conn.Enabled {
			apiKey := strings.TrimSpace(conn.APIKeyEnc)
			supplierID := strings.TrimSpace(conn.SupplierID)
			if apiKey == "" || supplierID == "" {
				return nil, "", fmt.Errorf("company topdawg connection is incomplete")
			}
			return providers.NewTopDawg(apiKey, supplierID), companyID, nil
		}

		if cjConn != nil {
			return nil, "", fmt.Errorf("company cjdropshipping connection is disabled")
		}
		if conn != nil {
			return nil, "", fmt.Errorf("company topdawg connection is disabled")
		}
	}
	client, err := getSupplierClientFromEnv()
	if err != nil {
		return nil, "", err
	}
	return client, companyID, nil
}

func annotateSupplierResult(result any, companyID string) any {
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

func isSupplierTool(toolName string) bool {
	_, ok := supplierToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callSupplierTools(ctx context.Context, agentID, toolName string, inputJSON []byte) (bool, any, error) {
	if !isSupplierTool(toolName) {
		return false, nil, nil
	}

	client, companyID, err := h.supplierClientForAgent(ctx, agentID)
	if err != nil {
		return true, nil, err
	}

	handler, ok := supplierToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}

	result, callErr := handler(ctx, client, inputJSON)
	return true, annotateSupplierResult(result, companyID), callErr
}
