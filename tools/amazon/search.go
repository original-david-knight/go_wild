package amazon

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// AmazonTools provides tools for searching the Amazon product catalog.
type AmazonTools struct {
	client *PAAClient
}

// NewAmazonTools creates a new AmazonTools instance.
func NewAmazonTools(client *PAAClient) *AmazonTools {
	return &AmazonTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *AmazonTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"amazon_search":      "Search Amazon's product catalog by keywords. Returns ASINs, prices, Prime eligibility, availability, and affiliate URLs. Use this to find compatible products, verify ASINs, and identify the best-rated items for affiliate linking.",
		"amazon_get_product": "Look up one or more Amazon products by ASIN. Returns current price, availability, Prime status, and affiliate URL.",
	}
	return descriptions[name]
}

// SearchInput defines input for searching Amazon products.
type SearchInput struct {
	Keywords string `json:"keywords" description:"Search terms (e.g. 'Pet Water Fountain Filters 3.2L')" required:"true"`
	Category string `json:"category" description:"Amazon search index/category (e.g. PetSupplies, Electronics, All). Defaults to All." enum:"All,Apparel,Automotive,Baby,Beauty,Books,Computers,Electronics,GardenAndOutdoor,GroceryAndGourmetFood,HealthPersonalCare,HomeAndKitchen,KindleStore,MusicalInstruments,OfficeProducts,PetSupplies,SportsAndOutdoors,ToolsAndHomeImprovement,ToysAndGames"`
	Limit    int    `json:"limit" description:"Max results (1-10, default 5)"`
	MinPrice int    `json:"min_price" description:"Minimum price in cents (e.g. 500 = $5.00)"`
	MaxPrice int    `json:"max_price" description:"Maximum price in cents (e.g. 5000 = $50.00)"`
	SortBy   string `json:"sort_by" description:"Sort order" enum:"AvgCustomerReviews,Featured,NewestArrivals,Price:HighToLow,Price:LowToHigh,Relevance"`
}

// AmazonSearchTool searches the Amazon product catalog.
func (t *AmazonTools) AmazonSearchTool(ctx context.Context, input SearchInput) (*loop.ToolResult, error) {
	result, err := t.client.SearchItems(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// GetProductInput defines input for looking up products by ASIN.
type GetProductInput struct {
	ASINs []string `json:"asins" description:"One or more Amazon ASINs to look up (max 10)" required:"true"`
}

// AmazonGetProductTool looks up products by ASIN.
func (t *AmazonTools) AmazonGetProductTool(ctx context.Context, input GetProductInput) (*loop.ToolResult, error) {
	result, err := t.client.GetItems(ctx, input.ASINs)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}
