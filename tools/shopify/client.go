package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// toGID ensures an ID is a full Shopify GID. If already prefixed, returns as-is.
func toGID(resource, id string) string {
	prefix := "gid://shopify/" + resource + "/"
	if strings.HasPrefix(id, prefix) {
		return id
	}
	// Strip any gid:// prefix with a different or duplicate resource path.
	if strings.HasPrefix(id, "gid://shopify/") {
		parts := strings.SplitN(id, "/", 5)
		if len(parts) >= 5 {
			return prefix + parts[4]
		}
	}
	return prefix + id
}

// ShopifyClient provides HTTP access to the Shopify Admin API.
// GraphQL-first with REST fallback for endpoints not available in GraphQL.
type ShopifyClient struct {
	shopURL    string // e.g. "my-store.myshopify.com"
	apiVersion string // e.g. "2025-01"
	token      string // Admin API access token
	http       *http.Client
}

// NewShopifyClient creates a new Shopify Admin API client.
func NewShopifyClient(shopURL, apiVersion, token string) *ShopifyClient {
	return &ShopifyClient{
		shopURL:    shopURL,
		apiVersion: apiVersion,
		token:      token,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// graphqlURL returns the GraphQL endpoint URL.
func (c *ShopifyClient) graphqlURL() string {
	return fmt.Sprintf("https://%s/admin/api/%s/graphql.json", c.shopURL, c.apiVersion)
}

// restURL returns a REST endpoint URL for the given resource path.
func (c *ShopifyClient) restURL(resource string) string {
	return fmt.Sprintf("https://%s/admin/api/%s/%s", c.shopURL, c.apiVersion, resource)
}

// graphqlRequest executes a GraphQL query and returns the parsed response.
func (c *ShopifyClient) graphqlRequest(ctx context.Context, query string, variables map[string]any) (map[string]any, error) {
	body := map[string]any{
		"query": query,
	}
	if variables != nil {
		body["variables"] = variables
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphqlURL(), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("shopify graphql request failed: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("shopify error (%d): %s", resp.StatusCode, string(respData))
	}

	var result map[string]any
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for GraphQL errors
	if errors, ok := result["errors"]; ok {
		return nil, fmt.Errorf("shopify graphql errors: %v", errors)
	}

	if data, ok := result["data"].(map[string]any); ok {
		return data, nil
	}
	return result, nil
}

// restRequest executes a REST API request.
func (c *ShopifyClient) restRequest(ctx context.Context, method, resource string, body any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.restURL(resource), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("shopify rest request failed: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("shopify error (%d): %s", resp.StatusCode, string(respData))
	}

	// DELETE responses may be empty
	if len(respData) == 0 {
		return map[string]any{"success": true}, nil
	}

	var result map[string]any
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// --- Product methods ---

func (c *ShopifyClient) CreateProduct(ctx context.Context, input CreateProductInput) (map[string]any, error) {
	query := `mutation productCreate($input: ProductInput!) {
		productCreate(input: $input) {
			product {
				id
				title
				handle
				status
				onlineStoreUrl
				variants(first: 1) {
					nodes {
						id
					}
				}
			}
			userErrors {
				field
				message
			}
		}
	}`

	productInput := map[string]any{
		"title":           input.Title,
		"descriptionHtml": input.BodyHTML,
		"vendor":          input.Vendor,
		"productType":     input.ProductType,
	}
	if len(input.Tags) > 0 {
		productInput["tags"] = input.Tags
	}

	data, err := c.graphqlRequest(ctx, query, map[string]any{"input": productInput})
	if err != nil {
		return nil, err
	}

	create, ok := data["productCreate"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}
	if errs, ok := create["userErrors"].([]any); ok && len(errs) > 0 {
		return nil, fmt.Errorf("shopify validation errors: %v", errs)
	}

	product, _ := create["product"].(map[string]any)

	// Update the default variant with pricing info
	if product != nil && (input.Price != "" || input.CompareAt != "" || input.SKU != "") {
		productID, _ := product["id"].(string)
		variantID := extractFirstVariantID(product)
		if productID != "" && variantID != "" {
			if err := c.updateVariantPricing(ctx, productID, variantID, input.Price, input.CompareAt, input.SKU); err != nil {
				return product, fmt.Errorf("product created but variant pricing failed: %w", err)
			}
		}
	}

	// Publish to the Online Store sales channel so the product is visible on the website.
	if product != nil {
		productID, _ := product["id"].(string)
		if productID != "" {
			if err := c.publishToOnlineStore(ctx, productID); err != nil {
				return product, fmt.Errorf("product created but publish failed: %w", err)
			}
		}
	}

	return product, nil
}

// extractFirstVariantID gets the default variant ID from a product response.
func extractFirstVariantID(product map[string]any) string {
	variants, _ := product["variants"].(map[string]any)
	nodes, _ := variants["nodes"].([]any)
	if len(nodes) == 0 {
		return ""
	}
	first, _ := nodes[0].(map[string]any)
	id, _ := first["id"].(string)
	return id
}

// updateVariantPricing updates a variant's price, compareAtPrice, and SKU.
func (c *ShopifyClient) updateVariantPricing(ctx context.Context, productID, variantID, price, compareAt, sku string) error {
	query := `mutation productVariantsBulkUpdate($productId: ID!, $variants: [ProductVariantsBulkInput!]!) {
		productVariantsBulkUpdate(productId: $productId, variants: $variants) {
			productVariants {
				id
			}
			userErrors {
				field
				message
			}
		}
	}`

	variant := map[string]any{
		"id": variantID,
	}
	if price != "" {
		variant["price"] = price
	}
	if compareAt != "" {
		variant["compareAtPrice"] = compareAt
	}
	if sku != "" {
		variant["inventoryItem"] = map[string]any{"sku": sku}
	}

	data, err := c.graphqlRequest(ctx, query, map[string]any{
		"productId": productID,
		"variants":  []map[string]any{variant},
	})
	if err != nil {
		return err
	}

	update, ok := data["productVariantsBulkUpdate"].(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected variant update response")
	}
	if errs, ok := update["userErrors"].([]any); ok && len(errs) > 0 {
		return fmt.Errorf("variant update errors: %v", errs)
	}
	return nil
}

// getOnlineStorePublicationID finds the publication ID for the "Online Store" channel.
func (c *ShopifyClient) getOnlineStorePublicationID(ctx context.Context) (string, error) {
	query := `{
		publications(first: 20) {
			nodes {
				id
				name
			}
		}
	}`
	data, err := c.graphqlRequest(ctx, query, nil)
	if err != nil {
		return "", err
	}
	pubs, _ := data["publications"].(map[string]any)
	nodes, _ := pubs["nodes"].([]any)
	for _, n := range nodes {
		node, _ := n.(map[string]any)
		name, _ := node["name"].(string)
		if name == "Online Store" {
			id, _ := node["id"].(string)
			return id, nil
		}
	}
	return "", fmt.Errorf("Online Store publication not found")
}

// publishToOnlineStore publishes a product to the Online Store sales channel.
func (c *ShopifyClient) publishToOnlineStore(ctx context.Context, productID string) error {
	pubID, err := c.getOnlineStorePublicationID(ctx)
	if err != nil {
		return err
	}

	query := `mutation publishablePublish($id: ID!, $input: [PublicationInput!]!) {
		publishablePublish(id: $id, input: $input) {
			userErrors {
				field
				message
			}
		}
	}`

	data, err := c.graphqlRequest(ctx, query, map[string]any{
		"id":    productID,
		"input": []map[string]any{{"publicationId": pubID}},
	})
	if err != nil {
		return err
	}

	pub, ok := data["publishablePublish"].(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected publish response")
	}
	if errs, ok := pub["userErrors"].([]any); ok && len(errs) > 0 {
		return fmt.Errorf("publish errors: %v", errs)
	}
	return nil
}

func (c *ShopifyClient) UpdateProduct(ctx context.Context, input UpdateProductInput) (map[string]any, error) {
	query := `mutation productUpdate($input: ProductInput!) {
		productUpdate(input: $input) {
			product {
				id
				title
				handle
				status
				tags
				updatedAt
				metafields(first: 20) {
					nodes {
						namespace
						key
						value
						type
					}
				}
			}
			userErrors {
				field
				message
			}
		}
	}`

	productInput := map[string]any{
		"id": toGID("Product", input.ProductID),
	}
	if input.Title != "" {
		productInput["title"] = input.Title
	}
	if input.BodyHTML != "" {
		productInput["descriptionHtml"] = input.BodyHTML
	}
	if input.Vendor != "" {
		productInput["vendor"] = input.Vendor
	}
	if input.ProductType != "" {
		productInput["productType"] = input.ProductType
	}
	if len(input.Tags) > 0 {
		productInput["tags"] = input.Tags
	}
	if input.Status != "" {
		productInput["status"] = input.Status
	}
	if len(input.Metafields) > 0 {
		mfs := make([]map[string]any, len(input.Metafields))
		for i, mf := range input.Metafields {
			mfs[i] = map[string]any{
				"namespace": mf.Namespace,
				"key":       mf.Key,
				"value":     mf.Value,
				"type":      mf.Type,
			}
		}
		productInput["metafields"] = mfs
	}

	data, err := c.graphqlRequest(ctx, query, map[string]any{"input": productInput})
	if err != nil {
		return nil, err
	}

	update, ok := data["productUpdate"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}
	if errs, ok := update["userErrors"].([]any); ok && len(errs) > 0 {
		return nil, fmt.Errorf("shopify validation errors: %v", errs)
	}

	product, _ := update["product"].(map[string]any)
	return product, nil
}

func (c *ShopifyClient) GetProduct(ctx context.Context, input GetProductInput) (map[string]any, error) {
	query := `query getProduct($id: ID!) {
		product(id: $id) {
			id
			title
			handle
			bodyHtml
			vendor
			productType
			status
			tags
			onlineStoreUrl
			totalInventory
			createdAt
			updatedAt
			variants(first: 50) {
				edges {
					node {
						id
						title
						price
						compareAtPrice
						sku
						inventoryQuantity
						inventoryItem { id }
					}
				}
			}
			images(first: 20) {
				edges {
					node {
						id
						url
						altText
					}
				}
			}
		}
	}`

	gid := toGID("Product", input.ProductID)
	data, err := c.graphqlRequest(ctx, query, map[string]any{"id": gid})
	if err != nil {
		return nil, err
	}

	product, ok := data["product"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("product not found")
	}
	return product, nil
}

func (c *ShopifyClient) ListProducts(ctx context.Context, input ListProductsInput) (map[string]any, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	queryFilter := ""
	if input.Status != "" {
		queryFilter += fmt.Sprintf("status:%s", input.Status)
	}
	if input.ProductType != "" {
		if queryFilter != "" {
			queryFilter += " AND "
		}
		queryFilter += fmt.Sprintf("product_type:%s", input.ProductType)
	}

	query := `query listProducts($first: Int!, $query: String, $after: String) {
		products(first: $first, query: $query, after: $after) {
			edges {
				node {
					id
					title
					handle
					vendor
					productType
					status
					totalInventory
					createdAt
				}
				cursor
			}
			pageInfo {
				hasNextPage
			}
		}
	}`

	vars := map[string]any{"first": limit}
	if queryFilter != "" {
		vars["query"] = queryFilter
	}
	if input.Cursor != "" {
		vars["after"] = input.Cursor
	}

	data, err := c.graphqlRequest(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (c *ShopifyClient) DeleteProduct(ctx context.Context, input DeleteProductInput) (map[string]any, error) {
	query := `mutation productDelete($input: ProductDeleteInput!) {
		productDelete(input: $input) {
			deletedProductId
			userErrors {
				field
				message
			}
		}
	}`

	gid := toGID("Product", input.ProductID)
	data, err := c.graphqlRequest(ctx, query, map[string]any{
		"input": map[string]any{"id": gid},
	})
	if err != nil {
		return nil, err
	}

	del, ok := data["productDelete"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}
	if errs, ok := del["userErrors"].([]any); ok && len(errs) > 0 {
		return nil, fmt.Errorf("shopify validation errors: %v", errs)
	}
	return del, nil
}

// --- Variant methods ---

func (c *ShopifyClient) UpdateVariant(ctx context.Context, input UpdateVariantInput) (map[string]any, error) {
	// Use REST for variant updates (simpler than GraphQL productVariantUpdate)
	variantData := map[string]any{}
	if input.Price != "" {
		variantData["price"] = input.Price
	}
	if input.CompareAt != "" {
		variantData["compare_at_price"] = input.CompareAt
	}
	if input.SKU != "" {
		variantData["sku"] = input.SKU
	}

	resource := fmt.Sprintf("variants/%s.json", input.VariantID)
	return c.restRequest(ctx, http.MethodPut, resource, map[string]any{"variant": variantData})
}

func (c *ShopifyClient) ListVariants(ctx context.Context, input ListVariantsInput) (map[string]any, error) {
	query := `query getProductVariants($id: ID!, $first: Int!) {
		product(id: $id) {
			variants(first: $first) {
				edges {
					node {
						id
						title
						price
						compareAtPrice
						sku
						inventoryQuantity
						selectedOptions {
							name
							value
						}
					}
				}
			}
		}
	}`

	limit := input.Limit
	if limit == 0 {
		limit = 50
	}

	gid := toGID("Product", input.ProductID)
	data, err := c.graphqlRequest(ctx, query, map[string]any{"id": gid, "first": limit})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// --- Order methods ---

func (c *ShopifyClient) ListOrders(ctx context.Context, input ListOrdersInput) (map[string]any, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	queryFilter := ""
	if input.Status != "" {
		queryFilter += fmt.Sprintf("status:%s", input.Status)
	}
	if input.FulfillmentStatus != "" {
		if queryFilter != "" {
			queryFilter += " AND "
		}
		queryFilter += fmt.Sprintf("fulfillment_status:%s", input.FulfillmentStatus)
	}

	query := `query listOrders($first: Int!, $query: String, $after: String) {
		orders(first: $first, query: $query, after: $after) {
			edges {
				node {
					id
					name
					email
					totalPriceSet { shopMoney { amount currencyCode } }
					displayFulfillmentStatus
					displayFinancialStatus
					createdAt
					lineItems(first: 20) {
						edges {
							node {
								id
								title
								quantity
								variant { id sku }
							}
						}
					}
					shippingAddress {
						name
						address1
						city
						province
						zip
						country
					}
				}
				cursor
			}
			pageInfo {
				hasNextPage
			}
		}
	}`

	vars := map[string]any{"first": limit}
	if queryFilter != "" {
		vars["query"] = queryFilter
	}
	if input.Cursor != "" {
		vars["after"] = input.Cursor
	}

	return c.graphqlRequest(ctx, query, vars)
}

func (c *ShopifyClient) GetOrder(ctx context.Context, input GetOrderInput) (map[string]any, error) {
	query := `query getOrder($id: ID!) {
		order(id: $id) {
			id
			name
			email
			phone
			totalPriceSet { shopMoney { amount currencyCode } }
			subtotalPriceSet { shopMoney { amount currencyCode } }
			totalTaxSet { shopMoney { amount currencyCode } }
			totalShippingPriceSet { shopMoney { amount currencyCode } }
			displayFulfillmentStatus
			displayFinancialStatus
			note
			tags
			createdAt
			updatedAt
			lineItems(first: 50) {
				edges {
					node {
						id
						title
						quantity
						originalUnitPriceSet { shopMoney { amount currencyCode } }
						variant { id sku }
					}
				}
			}
			shippingAddress {
				name
				address1
				address2
				city
				province
				zip
				country
				phone
			}
			fulfillments {
				id
				status
				trackingInfo { number url company }
			}
		}
	}`

	gid := toGID("Order", input.OrderID)
	data, err := c.graphqlRequest(ctx, query, map[string]any{"id": gid})
	if err != nil {
		return nil, err
	}

	order, ok := data["order"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("order not found")
	}
	return order, nil
}

func (c *ShopifyClient) UpdateOrder(ctx context.Context, input UpdateOrderInput) (map[string]any, error) {
	// Use REST for order updates (note, tags)
	orderData := map[string]any{}
	if input.Note != "" {
		orderData["note"] = input.Note
	}
	if len(input.Tags) > 0 {
		orderData["tags"] = input.Tags
	}

	resource := fmt.Sprintf("orders/%s.json", input.OrderID)
	return c.restRequest(ctx, http.MethodPut, resource, map[string]any{"order": orderData})
}

func (c *ShopifyClient) CreateFulfillment(ctx context.Context, input CreateFulfillmentInput) (map[string]any, error) {
	query := `mutation fulfillmentCreateV2($fulfillment: FulfillmentV2Input!) {
		fulfillmentCreateV2(fulfillment: $fulfillment) {
			fulfillment {
				id
				status
				trackingInfo { number url company }
			}
			userErrors {
				field
				message
			}
		}
	}`

	fulfillment := map[string]any{
		"lineItemsByFulfillmentOrder": []map[string]any{
			{"fulfillmentOrderId": toGID("FulfillmentOrder", input.OrderID)},
		},
	}
	if input.TrackingNumber != "" {
		fulfillment["trackingInfo"] = map[string]any{
			"number":  input.TrackingNumber,
			"url":     input.TrackingURL,
			"company": input.TrackingCompany,
		}
	}
	if input.NotifyCustomer {
		fulfillment["notifyCustomer"] = true
	}

	data, err := c.graphqlRequest(ctx, query, map[string]any{"fulfillment": fulfillment})
	if err != nil {
		return nil, err
	}

	create, ok := data["fulfillmentCreateV2"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}
	if errs, ok := create["userErrors"].([]any); ok && len(errs) > 0 {
		return nil, fmt.Errorf("shopify validation errors: %v", errs)
	}

	fulfillmentResult, _ := create["fulfillment"].(map[string]any)
	return fulfillmentResult, nil
}

// --- Customer methods ---

func (c *ShopifyClient) GetCustomer(ctx context.Context, input GetCustomerInput) (map[string]any, error) {
	query := `query getCustomer($id: ID!) {
		customer(id: $id) {
			id
			firstName
			lastName
			email
			phone
			ordersCount
			totalSpentV2 { amount currencyCode }
			tags
			createdAt
			updatedAt
			addresses {
				address1
				city
				province
				zip
				country
			}
		}
	}`

	gid := toGID("Customer", input.CustomerID)
	data, err := c.graphqlRequest(ctx, query, map[string]any{"id": gid})
	if err != nil {
		return nil, err
	}

	customer, ok := data["customer"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("customer not found")
	}
	return customer, nil
}

func (c *ShopifyClient) ListCustomers(ctx context.Context, input ListCustomersInput) (map[string]any, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	query := `query listCustomers($first: Int!, $query: String, $after: String) {
		customers(first: $first, query: $query, after: $after) {
			edges {
				node {
					id
					firstName
					lastName
					email
					ordersCount
					totalSpentV2 { amount currencyCode }
					createdAt
				}
				cursor
			}
			pageInfo {
				hasNextPage
			}
		}
	}`

	vars := map[string]any{"first": limit}
	if input.Cursor != "" {
		vars["after"] = input.Cursor
	}

	return c.graphqlRequest(ctx, query, vars)
}

func (c *ShopifyClient) SearchCustomers(ctx context.Context, input SearchCustomersInput) (map[string]any, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	query := `query searchCustomers($first: Int!, $query: String!) {
		customers(first: $first, query: $query) {
			edges {
				node {
					id
					firstName
					lastName
					email
					ordersCount
					totalSpentV2 { amount currencyCode }
				}
			}
		}
	}`

	return c.graphqlRequest(ctx, query, map[string]any{
		"first": limit,
		"query": input.Query,
	})
}

// --- Inventory methods ---

func (c *ShopifyClient) GetInventoryLevel(ctx context.Context, input GetInventoryLevelInput) (map[string]any, error) {
	// Use REST for inventory levels
	resource := fmt.Sprintf("inventory_levels.json?inventory_item_ids=%s", input.InventoryItemID)
	return c.restRequest(ctx, http.MethodGet, resource, nil)
}

func (c *ShopifyClient) SetInventoryLevel(ctx context.Context, input SetInventoryLevelInput) (map[string]any, error) {
	query := `mutation inventorySetOnHandQuantities($input: InventorySetOnHandQuantitiesInput!) {
		inventorySetOnHandQuantities(input: $input) {
			inventoryAdjustmentGroup {
				reason
			}
			userErrors {
				field
				message
			}
		}
	}`

	invInput := map[string]any{
		"reason": "other",
		"setQuantities": []map[string]any{
			{
				"inventoryItemId": toGID("InventoryItem", input.InventoryItemID),
				"locationId":      toGID("Location", input.LocationID),
				"quantity":        input.Quantity,
			},
		},
	}

	data, err := c.graphqlRequest(ctx, query, map[string]any{"input": invInput})
	if err != nil {
		return nil, err
	}

	result, ok := data["inventorySetOnHandQuantities"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}
	if errs, ok := result["userErrors"].([]any); ok && len(errs) > 0 {
		return nil, fmt.Errorf("shopify validation errors: %v", errs)
	}
	return result, nil
}

// --- Analytics methods ---

func (c *ShopifyClient) GetReports(ctx context.Context, input GetReportsInput) (map[string]any, error) {
	// Use REST for reports
	resource := "reports.json"
	if input.Limit > 0 {
		resource = fmt.Sprintf("reports.json?limit=%d", input.Limit)
	}
	return c.restRequest(ctx, http.MethodGet, resource, nil)
}

func (c *ShopifyClient) GetOrdersSummary(ctx context.Context, input GetOrdersSummaryInput) (map[string]any, error) {
	// Query orders within date range and compute summary
	queryFilter := fmt.Sprintf("created_at:>=%s AND created_at:<=%s", input.DateFrom, input.DateTo)

	query := `query ordersSummary($query: String!) {
		orders(first: 250, query: $query) {
			edges {
				node {
					totalPriceSet { shopMoney { amount currencyCode } }
					displayFinancialStatus
					displayFulfillmentStatus
				}
			}
		}
	}`

	return c.graphqlRequest(ctx, query, map[string]any{"query": queryFilter})
}

// --- Image methods ---

func (c *ShopifyClient) UploadImage(ctx context.Context, input UploadImageInput) (map[string]any, error) {
	// Use REST for image creation
	imageData := map[string]any{
		"src": input.ImageURL,
	}
	if input.AltText != "" {
		imageData["alt"] = input.AltText
	}
	if input.Position > 0 {
		imageData["position"] = input.Position
	}

	resource := fmt.Sprintf("products/%s/images.json", input.ProductID)
	return c.restRequest(ctx, http.MethodPost, resource, map[string]any{"image": imageData})
}

func (c *ShopifyClient) ListImages(ctx context.Context, input ListImagesInput) (map[string]any, error) {
	query := `query getProductImages($id: ID!, $first: Int!) {
		product(id: $id) {
			images(first: $first) {
				edges {
					node {
						id
						url
						altText
						width
						height
					}
				}
			}
		}
	}`

	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	gid := toGID("Product", input.ProductID)
	return c.graphqlRequest(ctx, query, map[string]any{"id": gid, "first": limit})
}

// --- Location methods ---

// ListLocations returns the store's active locations.
func (c *ShopifyClient) ListLocations(ctx context.Context) (map[string]any, error) {
	query := `query { locations(first: 10) { edges { node { id name isActive } } } }`
	return c.graphqlRequest(ctx, query, nil)
}

// --- Theme & Asset methods ---

func (c *ShopifyClient) ListThemes(ctx context.Context) (map[string]any, error) {
	return c.restRequest(ctx, http.MethodGet, "themes.json", nil)
}

func (c *ShopifyClient) GetTheme(ctx context.Context, input GetThemeInput) (map[string]any, error) {
	resource := fmt.Sprintf("themes/%s.json", input.ThemeID)
	return c.restRequest(ctx, http.MethodGet, resource, nil)
}

func (c *ShopifyClient) ListAssets(ctx context.Context, input ListAssetsInput) (map[string]any, error) {
	resource := fmt.Sprintf("themes/%s/assets.json", input.ThemeID)
	return c.restRequest(ctx, http.MethodGet, resource, nil)
}

func (c *ShopifyClient) GetAsset(ctx context.Context, input GetAssetInput) (map[string]any, error) {
	resource := fmt.Sprintf("themes/%s/assets.json?asset[key]=%s", input.ThemeID, url.QueryEscape(input.AssetKey))
	return c.restRequest(ctx, http.MethodGet, resource, nil)
}

func (c *ShopifyClient) UpdateAsset(ctx context.Context, input UpdateAssetInput) (map[string]any, error) {
	resource := fmt.Sprintf("themes/%s/assets.json", input.ThemeID)
	body := map[string]any{
		"asset": map[string]any{
			"key":   input.AssetKey,
			"value": input.Value,
		},
	}
	return c.restRequest(ctx, http.MethodPut, resource, body)
}

func (c *ShopifyClient) DeleteAsset(ctx context.Context, input DeleteAssetInput) (map[string]any, error) {
	resource := fmt.Sprintf("themes/%s/assets.json?asset[key]=%s", input.ThemeID, url.QueryEscape(input.AssetKey))
	return c.restRequest(ctx, http.MethodDelete, resource, nil)
}

// --- Page methods ---

func (c *ShopifyClient) ListPages(ctx context.Context, input ListPagesInput) (map[string]any, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	resource := fmt.Sprintf("pages.json?limit=%d", limit)
	return c.restRequest(ctx, http.MethodGet, resource, nil)
}

func (c *ShopifyClient) GetPage(ctx context.Context, input GetPageInput) (map[string]any, error) {
	resource := fmt.Sprintf("pages/%s.json", input.PageID)
	return c.restRequest(ctx, http.MethodGet, resource, nil)
}

func (c *ShopifyClient) CreatePage(ctx context.Context, input CreatePageInput) (map[string]any, error) {
	body := map[string]any{
		"page": map[string]any{
			"title":     input.Title,
			"body_html": input.BodyHTML,
			"published": input.Published,
		},
	}
	return c.restRequest(ctx, http.MethodPost, "pages.json", body)
}

func (c *ShopifyClient) UpdatePage(ctx context.Context, input UpdatePageInput) (map[string]any, error) {
	pageData := map[string]any{}
	if input.Title != "" {
		pageData["title"] = input.Title
	}
	if input.BodyHTML != "" {
		pageData["body_html"] = input.BodyHTML
	}
	if input.Published != nil {
		pageData["published"] = *input.Published
	}
	resource := fmt.Sprintf("pages/%s.json", input.PageID)
	return c.restRequest(ctx, http.MethodPut, resource, map[string]any{"page": pageData})
}

func (c *ShopifyClient) DeletePage(ctx context.Context, input DeletePageInput) (map[string]any, error) {
	resource := fmt.Sprintf("pages/%s.json", input.PageID)
	return c.restRequest(ctx, http.MethodDelete, resource, nil)
}
