package ecommerce

// PnLTools provides P&L analytics tools for e-commerce.
// P&L computation happens on the manager side (it has DB access);
// these structs define the tool interface for the agent.
type PnLTools struct{}

// NewPnLTools creates a new PnLTools instance.
func NewPnLTools() *PnLTools {
	return &PnLTools{}
}

// DescribeTool returns the description for a tool by name.
func (t *PnLTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"ecommerce_product_pnl": "Get profit-and-loss breakdown for a specific Shopify product over a date range. Combines revenue, supplier costs, ad spend, and Shopify fees to compute net profit, margin, and ROAS.",
		"ecommerce_daily_pnl":   "Get daily profit-and-loss summary across all products for a date range. Shows total revenue, COGS, ad spend, fees, net profit, margin, and order/return counts per day.",
	}
	return descriptions[name]
}

// ProductPnLInput defines input for per-product P&L.
type ProductPnLInput struct {
	ShopifyProductID string `json:"shopify_product_id" description:"Shopify product ID" required:"true"`
	DateFrom         string `json:"date_from" description:"Start date (YYYY-MM-DD)" required:"true"`
	DateTo           string `json:"date_to" description:"End date (YYYY-MM-DD)" required:"true"`
}

// DailyPnLInput defines input for daily P&L summary.
type DailyPnLInput struct {
	DateFrom string `json:"date_from" description:"Start date (YYYY-MM-DD)" required:"true"`
	DateTo   string `json:"date_to" description:"End date (YYYY-MM-DD)" required:"true"`
}
