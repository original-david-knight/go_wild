package shopify

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyCustomerTools provides tools for Shopify customer lookup.
type ShopifyCustomerTools struct {
	client *ShopifyClient
}

// NewShopifyCustomerTools creates a new ShopifyCustomerTools instance.
func NewShopifyCustomerTools(client *ShopifyClient) *ShopifyCustomerTools {
	return &ShopifyCustomerTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *ShopifyCustomerTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_get_customer":     "Get full details for a single customer including addresses and order history.",
		"shopify_list_customers":   "List customers with optional pagination.",
		"shopify_search_customers": "Search customers by email, name, or other attributes.",
	}
	return descriptions[name]
}

// GetCustomerInput defines input for getting a customer.
type GetCustomerInput struct {
	CustomerID string `json:"customer_id" description:"Shopify customer ID" required:"true"`
}

// ShopifyGetCustomerTool retrieves a single customer.
func (t *ShopifyCustomerTools) ShopifyGetCustomerTool(ctx context.Context, input GetCustomerInput) (*loop.ToolResult, error) {
	customer, err := t.client.GetCustomer(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(customer), nil
}

// ListCustomersInput defines input for listing customers.
type ListCustomersInput struct {
	Limit  int    `json:"limit" description:"Max customers to return (default 20, max 250)"`
	Cursor string `json:"cursor" description:"Pagination cursor from previous response"`
}

// ShopifyListCustomersTool lists customers in the store.
func (t *ShopifyCustomerTools) ShopifyListCustomersTool(ctx context.Context, input ListCustomersInput) (*loop.ToolResult, error) {
	data, err := t.client.ListCustomers(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(data), nil
}

// SearchCustomersInput defines input for searching customers.
type SearchCustomersInput struct {
	Query string `json:"query" description:"Search query (email, name, phone, etc.)" required:"true"`
	Limit int    `json:"limit" description:"Max results to return (default 20)"`
}

// ShopifySearchCustomersTool searches customers by query.
func (t *ShopifyCustomerTools) ShopifySearchCustomersTool(ctx context.Context, input SearchCustomersInput) (*loop.ToolResult, error) {
	data, err := t.client.SearchCustomers(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(data), nil
}
