package supplier

import (
	"context"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SupplierProductTools provides tools for searching and browsing supplier product catalogs.
type SupplierProductTools struct {
	client Supplier
}

// NewSupplierProductTools creates a new SupplierProductTools instance.
func NewSupplierProductTools(client Supplier) *SupplierProductTools {
	return &SupplierProductTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *SupplierProductTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"supplier_search_products": "Search the supplier catalog for products matching a query. Returns product listings with pricing, ratings, and shipping info. Use filters to narrow results by category, price range, rating, delivery time, and origin country. Note: some suppliers (e.g. CJ Dropshipping) do not provide rating, review count, or delivery estimates in search results — these fields will be -1 when unavailable. A price of 0 means the supplier did not provide pricing for that listing. Use supplier_get_product and supplier_get_shipping for more complete data on specific products.",
		"supplier_get_product":     "Get full details for a specific supplier product by ID. Returns complete product info including all variants, images, and shipping estimates. Rating/review fields are -1 when the supplier does not provide them.",
		"supplier_get_shipping":    "Get shipping cost and delivery time estimate for a product to a specific country. Returns method, cost, and estimated delivery window. This is the most reliable way to get delivery time and cost — use it instead of relying on est_delivery_days from search results.",
	}
	return descriptions[name]
}

// SearchProductsInput defines input for searching supplier products.
type SearchProductsInput struct {
	Query            string  `json:"query" description:"Search keywords" required:"true"`
	Category         string  `json:"category" description:"Product category filter"`
	MinPrice         float64 `json:"min_price" description:"Minimum price in USD"`
	MaxPrice         float64 `json:"max_price" description:"Maximum price in USD"`
	MinRating        float64 `json:"min_rating" description:"Minimum product rating (0-5)"`
	MaxDeliveryDays  int     `json:"max_delivery_days" description:"Maximum estimated delivery days"`
	ShipsFromCountry string  `json:"ships_from_country" description:"Preferred origin country (default US)"`
	SortBy           string  `json:"sort_by" description:"Sort order" enum:"price_asc,price_desc,rating,orders"`
	Page             int     `json:"page" description:"Page number for pagination"`
}

// SupplierSearchProductsTool searches the supplier catalog.
func (t *SupplierProductTools) SupplierSearchProductsTool(ctx context.Context, input SearchProductsInput) (*loop.ToolResult, error) {
	opts := SearchOpts{
		Category:         input.Category,
		MinPrice:         input.MinPrice,
		MaxPrice:         input.MaxPrice,
		MinRating:        input.MinRating,
		MaxDeliveryDays:  input.MaxDeliveryDays,
		ShipsFromCountry: input.ShipsFromCountry,
		SortBy:           input.SortBy,
		Page:             input.Page,
	}

	products, err := t.client.SearchProducts(ctx, input.Query, opts)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("supplier search failed: %v", err)), nil
	}

	// Count products missing key data so the agent understands data quality.
	missingPrice := 0
	missingDelivery := 0
	ratingsUnavailable := false
	for _, p := range products {
		if p.Price <= 0 {
			missingPrice++
		}
		if p.EstDeliveryDays <= 0 {
			missingDelivery++
		}
		if p.Rating < 0 {
			ratingsUnavailable = true
		}
	}

	result := map[string]any{
		"products": products,
		"count":    len(products),
		"page":     input.Page,
	}
	if missingPrice > 0 || missingDelivery > 0 || ratingsUnavailable {
		notes := []string{}
		if ratingsUnavailable {
			notes = append(notes, "This supplier does not provide rating or review data (shown as -1). Use other signals like price and shipping to evaluate products.")
		}
		if missingPrice > 0 {
			notes = append(notes, fmt.Sprintf("%d of %d products have no price listed. Use supplier_get_product on specific items to get variant-level pricing.", missingPrice, len(products)))
		}
		if missingDelivery > 0 {
			notes = append(notes, fmt.Sprintf("%d of %d products have no delivery estimate. Use supplier_get_shipping for accurate delivery times.", missingDelivery, len(products)))
		}
		result["data_quality_notes"] = notes
	}

	return loop.NewSuccessResult(result), nil
}

// GetProductInput defines input for getting a single product.
type GetProductInput struct {
	ProductID string `json:"product_id" description:"Supplier product ID" required:"true"`
}

// SupplierGetProductTool retrieves full details for a supplier product.
func (t *SupplierProductTools) SupplierGetProductTool(ctx context.Context, input GetProductInput) (*loop.ToolResult, error) {
	product, err := t.client.GetProduct(ctx, input.ProductID)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get product: %v", err)), nil
	}

	return loop.NewSuccessResult(product), nil
}

// GetShippingInput defines input for getting a shipping estimate.
type GetShippingInput struct {
	ProductID string `json:"product_id" description:"Supplier product ID" required:"true"`
	Country   string `json:"country" description:"Destination country ISO code (e.g. US, GB, CA)" required:"true"`
}

// SupplierGetShippingTool retrieves shipping cost and delivery estimate.
func (t *SupplierProductTools) SupplierGetShippingTool(ctx context.Context, input GetShippingInput) (*loop.ToolResult, error) {
	estimate, err := t.client.GetShippingEstimate(ctx, input.ProductID, input.Country)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get shipping estimate: %v", err)), nil
	}

	return loop.NewSuccessResult(estimate), nil
}
