package supplier

import "context"

// Supplier defines the interface for drop-shipping supplier integrations.
// TopDawg is the v1 primary provider; additional providers implement this same interface.
type Supplier interface {
	SearchProducts(ctx context.Context, query string, opts SearchOpts) ([]Product, error)
	GetProduct(ctx context.Context, productID string) (*Product, error)
	GetShippingEstimate(ctx context.Context, productID, country string) (*ShippingEstimate, error)
	PlaceOrder(ctx context.Context, order OrderRequest) (*OrderConfirmation, error)
	GetOrder(ctx context.Context, orderID string) (*OrderStatus, error)
	GetTracking(ctx context.Context, orderID string) (*TrackingInfo, error)
}

// SearchOpts contains filtering and pagination options for product search.
type SearchOpts struct {
	Category         string  `json:"category,omitempty"`
	MinPrice         float64 `json:"min_price,omitempty"`
	MaxPrice         float64 `json:"max_price,omitempty"`
	MinRating        float64 `json:"min_rating,omitempty"`
	MaxDeliveryDays  int     `json:"max_delivery_days,omitempty"`
	ShipsFromCountry string  `json:"ships_from_country,omitempty"`
	SortBy           string  `json:"sort_by,omitempty"`
	Page             int     `json:"page,omitempty"`
}

// Product represents a supplier product.
type Product struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Category        string    `json:"category"`
	Price           float64   `json:"price"`
	CompareAtPrice  float64   `json:"compare_at_price,omitempty"`
	Currency        string    `json:"currency"`
	Rating          float64   `json:"rating"`
	ReviewCount     int       `json:"review_count"`
	ImageURLs       []string  `json:"image_urls"`
	Variants        []Variant `json:"variants,omitempty"`
	ShipsFrom       string    `json:"ships_from"`
	EstDeliveryDays int       `json:"est_delivery_days"`
	SupplierName    string    `json:"supplier_name"`
}

// Variant represents a product variant (size, color, etc).
type Variant struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Price      float64 `json:"price"`
	SKU        string  `json:"sku"`
	InStock    bool    `json:"in_stock"`
	StockCount int     `json:"stock_count,omitempty"`
}

// ShippingEstimate contains estimated shipping cost and time.
type ShippingEstimate struct {
	ProductID    string  `json:"product_id"`
	Country      string  `json:"country"`
	Method       string  `json:"method"`
	Cost         float64 `json:"cost"`
	Currency     string  `json:"currency"`
	MinDays      int     `json:"min_days"`
	MaxDays      int     `json:"max_days"`
	TrackingAvail bool   `json:"tracking_available"`
}

// OrderRequest contains all fields needed to place a supplier order.
type OrderRequest struct {
	ProductID       string `json:"product_id"`
	VariantID       string `json:"variant_id"`
	Quantity        int    `json:"quantity"`
	ShippingName    string `json:"shipping_name"`
	ShippingAddress string `json:"shipping_address"`
	ShippingCity    string `json:"shipping_city"`
	ShippingState   string `json:"shipping_state"`
	ShippingZip     string `json:"shipping_zip"`
	ShippingCountry string `json:"shipping_country"`
	ShippingPhone   string `json:"shipping_phone"`
	ShopifyOrderID  string `json:"shopify_order_id,omitempty"`
}

// OrderConfirmation is returned after placing a supplier order.
type OrderConfirmation struct {
	OrderID        string  `json:"order_id"`
	Status         string  `json:"status"`
	Total          float64 `json:"total"`
	Currency       string  `json:"currency"`
	EstDeliveryMin int     `json:"est_delivery_min_days"`
	EstDeliveryMax int     `json:"est_delivery_max_days"`
}

// OrderStatus represents the current state of a supplier order.
type OrderStatus struct {
	OrderID        string  `json:"order_id"`
	Status         string  `json:"status"` // pending, processing, shipped, delivered, cancelled
	Total          float64 `json:"total"`
	Currency       string  `json:"currency"`
	TrackingNumber string  `json:"tracking_number,omitempty"`
	TrackingURL    string  `json:"tracking_url,omitempty"`
	ShippedAt      string  `json:"shipped_at,omitempty"`
	DeliveredAt    string  `json:"delivered_at,omitempty"`
}

// TrackingInfo contains shipment tracking details.
type TrackingInfo struct {
	OrderID        string         `json:"order_id"`
	TrackingNumber string         `json:"tracking_number"`
	Carrier        string         `json:"carrier"`
	Status         string         `json:"status"` // in_transit, out_for_delivery, delivered, exception
	TrackingURL    string         `json:"tracking_url"`
	EstDelivery    string         `json:"est_delivery,omitempty"`
	Events         []TrackingEvent `json:"events,omitempty"`
}

// TrackingEvent is a single tracking milestone.
type TrackingEvent struct {
	Timestamp   string `json:"timestamp"`
	Location    string `json:"location"`
	Description string `json:"description"`
	Status      string `json:"status"`
}
