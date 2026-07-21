package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/original-david-knight/go_wild/tools/supplier"
)

const topdawgBaseURL = "https://app.topdawg.com/api/v1"

// TopDawg implements the Supplier interface for the TopDawg drop-shipping platform.
type TopDawg struct {
	apiKey     string
	supplierID string
	httpClient *http.Client
}

// NewTopDawg creates a new TopDawg supplier client.
func NewTopDawg(apiKey, supplierID string) *TopDawg {
	return &TopDawg{
		apiKey:     apiKey,
		supplierID: supplierID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *TopDawg) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, topdawgBaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("TopDawg API error (%d): %s", resp.StatusCode, string(data))
	}

	return data, nil
}

// SearchProducts searches the TopDawg catalog.
func (t *TopDawg) SearchProducts(ctx context.Context, query string, opts supplier.SearchOpts) ([]supplier.Product, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("supplier_id", t.supplierID)
	if opts.Category != "" {
		params.Set("category", opts.Category)
	}
	if opts.MinPrice > 0 {
		params.Set("min_price", strconv.FormatFloat(opts.MinPrice, 'f', 2, 64))
	}
	if opts.MaxPrice > 0 {
		params.Set("max_price", strconv.FormatFloat(opts.MaxPrice, 'f', 2, 64))
	}
	if opts.SortBy != "" {
		params.Set("sort_by", opts.SortBy)
	}
	if opts.Page > 0 {
		params.Set("page", strconv.Itoa(opts.Page))
	}

	data, err := t.doRequest(ctx, http.MethodGet, "/products?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Products []tdProduct `json:"products"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse products: %w", err)
	}

	products := make([]supplier.Product, len(resp.Products))
	for i, p := range resp.Products {
		products[i] = p.toProduct()
	}

	// Apply client-side filters that the API may not support
	var filtered []supplier.Product
	for _, p := range products {
		if opts.MinRating > 0 && p.Rating < opts.MinRating {
			continue
		}
		if opts.MaxDeliveryDays > 0 && p.EstDeliveryDays > opts.MaxDeliveryDays {
			continue
		}
		if opts.ShipsFromCountry != "" && p.ShipsFrom != opts.ShipsFromCountry {
			continue
		}
		filtered = append(filtered, p)
	}

	return filtered, nil
}

// GetProduct retrieves a single product by ID.
func (t *TopDawg) GetProduct(ctx context.Context, productID string) (*supplier.Product, error) {
	data, err := t.doRequest(ctx, http.MethodGet, "/products/"+productID, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Product tdProduct `json:"product"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse product: %w", err)
	}

	product := resp.Product.toProduct()
	return &product, nil
}

// GetShippingEstimate retrieves shipping estimate for a product.
func (t *TopDawg) GetShippingEstimate(ctx context.Context, productID, country string) (*supplier.ShippingEstimate, error) {
	params := url.Values{}
	params.Set("country", country)

	data, err := t.doRequest(ctx, http.MethodGet, "/products/"+productID+"/shipping?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Shipping tdShipping `json:"shipping"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse shipping: %w", err)
	}

	return &supplier.ShippingEstimate{
		ProductID:     productID,
		Country:       country,
		Method:        resp.Shipping.Method,
		Cost:          resp.Shipping.Cost,
		Currency:      "USD",
		MinDays:       resp.Shipping.MinDays,
		MaxDays:       resp.Shipping.MaxDays,
		TrackingAvail: resp.Shipping.Tracking,
	}, nil
}

// PlaceOrder places a new order with TopDawg.
func (t *TopDawg) PlaceOrder(ctx context.Context, order supplier.OrderRequest) (*supplier.OrderConfirmation, error) {
	body := map[string]any{
		"product_id":      order.ProductID,
		"variant_id":      order.VariantID,
		"quantity":        order.Quantity,
		"shipping_name":   order.ShippingName,
		"shipping_line1":  order.ShippingAddress,
		"shipping_city":   order.ShippingCity,
		"shipping_state":  order.ShippingState,
		"shipping_zip":    order.ShippingZip,
		"shipping_country": order.ShippingCountry,
		"shipping_phone":  order.ShippingPhone,
	}
	if order.ShopifyOrderID != "" {
		body["external_order_id"] = order.ShopifyOrderID
	}

	data, err := t.doRequest(ctx, http.MethodPost, "/orders", body)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Order tdOrder `json:"order"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse order: %w", err)
	}

	return &supplier.OrderConfirmation{
		OrderID:        resp.Order.ID,
		Status:         resp.Order.Status,
		Total:          resp.Order.Total,
		Currency:       "USD",
		EstDeliveryMin: resp.Order.EstMinDays,
		EstDeliveryMax: resp.Order.EstMaxDays,
	}, nil
}

// GetOrder retrieves the status of an existing order.
func (t *TopDawg) GetOrder(ctx context.Context, orderID string) (*supplier.OrderStatus, error) {
	data, err := t.doRequest(ctx, http.MethodGet, "/orders/"+orderID, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Order tdOrder `json:"order"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse order: %w", err)
	}

	return &supplier.OrderStatus{
		OrderID:        resp.Order.ID,
		Status:         resp.Order.Status,
		Total:          resp.Order.Total,
		Currency:       "USD",
		TrackingNumber: resp.Order.TrackingNumber,
		TrackingURL:    resp.Order.TrackingURL,
		ShippedAt:      resp.Order.ShippedAt,
		DeliveredAt:    resp.Order.DeliveredAt,
	}, nil
}

// GetTracking retrieves tracking information for an order.
func (t *TopDawg) GetTracking(ctx context.Context, orderID string) (*supplier.TrackingInfo, error) {
	data, err := t.doRequest(ctx, http.MethodGet, "/orders/"+orderID+"/tracking", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Tracking tdTracking `json:"tracking"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tracking: %w", err)
	}

	events := make([]supplier.TrackingEvent, len(resp.Tracking.Events))
	for i, e := range resp.Tracking.Events {
		events[i] = supplier.TrackingEvent{
			Timestamp:   e.Timestamp,
			Location:    e.Location,
			Description: e.Description,
			Status:      e.Status,
		}
	}

	return &supplier.TrackingInfo{
		OrderID:        orderID,
		TrackingNumber: resp.Tracking.TrackingNumber,
		Carrier:        resp.Tracking.Carrier,
		Status:         resp.Tracking.Status,
		TrackingURL:    resp.Tracking.TrackingURL,
		EstDelivery:    resp.Tracking.EstDelivery,
		Events:         events,
	}, nil
}

// TopDawg API response types (internal)

type tdProduct struct {
	ID              string      `json:"id"`
	Title           string      `json:"title"`
	Description     string      `json:"description"`
	Category        string      `json:"category"`
	Price           float64     `json:"price"`
	CompareAtPrice  float64     `json:"compare_at_price"`
	Rating          float64     `json:"rating"`
	ReviewCount     int         `json:"review_count"`
	Images          []string    `json:"images"`
	Variants        []tdVariant `json:"variants"`
	ShipsFrom       string      `json:"ships_from"`
	EstDeliveryDays int         `json:"est_delivery_days"`
}

func (p tdProduct) toProduct() supplier.Product {
	variants := make([]supplier.Variant, len(p.Variants))
	for i, v := range p.Variants {
		variants[i] = supplier.Variant{
			ID:         v.ID,
			Title:      v.Title,
			Price:      v.Price,
			SKU:        v.SKU,
			InStock:    v.InStock,
			StockCount: v.StockCount,
		}
	}
	return supplier.Product{
		ID:              p.ID,
		Title:           p.Title,
		Description:     p.Description,
		Category:        p.Category,
		Price:           p.Price,
		CompareAtPrice:  p.CompareAtPrice,
		Currency:        "USD",
		Rating:          p.Rating,
		ReviewCount:     p.ReviewCount,
		ImageURLs:       p.Images,
		Variants:        variants,
		ShipsFrom:       p.ShipsFrom,
		EstDeliveryDays: p.EstDeliveryDays,
		SupplierName:    "topdawg",
	}
}

type tdVariant struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Price      float64 `json:"price"`
	SKU        string  `json:"sku"`
	InStock    bool    `json:"in_stock"`
	StockCount int     `json:"stock_count"`
}

type tdShipping struct {
	Method   string  `json:"method"`
	Cost     float64 `json:"cost"`
	MinDays  int     `json:"min_days"`
	MaxDays  int     `json:"max_days"`
	Tracking bool    `json:"tracking"`
}

type tdOrder struct {
	ID             string  `json:"id"`
	Status         string  `json:"status"`
	Total          float64 `json:"total"`
	TrackingNumber string  `json:"tracking_number"`
	TrackingURL    string  `json:"tracking_url"`
	ShippedAt      string  `json:"shipped_at"`
	DeliveredAt    string  `json:"delivered_at"`
	EstMinDays     int     `json:"est_min_days"`
	EstMaxDays     int     `json:"est_max_days"`
}

type tdTracking struct {
	TrackingNumber string           `json:"tracking_number"`
	Carrier        string           `json:"carrier"`
	Status         string           `json:"status"`
	TrackingURL    string           `json:"tracking_url"`
	EstDelivery    string           `json:"est_delivery"`
	Events         []tdTrackingEvent `json:"events"`
}

type tdTrackingEvent struct {
	Timestamp   string `json:"timestamp"`
	Location    string `json:"location"`
	Description string `json:"description"`
	Status      string `json:"status"`
}
