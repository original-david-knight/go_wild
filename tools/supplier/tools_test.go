package supplier

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubSupplier struct {
	searchProductsFn func(context.Context, string, SearchOpts) ([]Product, error)
	getProductFn     func(context.Context, string) (*Product, error)
	getShippingFn    func(context.Context, string, string) (*ShippingEstimate, error)
	placeOrderFn     func(context.Context, OrderRequest) (*OrderConfirmation, error)
	getOrderFn       func(context.Context, string) (*OrderStatus, error)
	getTrackingFn    func(context.Context, string) (*TrackingInfo, error)
}

func (s stubSupplier) SearchProducts(ctx context.Context, query string, opts SearchOpts) ([]Product, error) {
	if s.searchProductsFn != nil {
		return s.searchProductsFn(ctx, query, opts)
	}
	return nil, nil
}

func (s stubSupplier) GetProduct(ctx context.Context, productID string) (*Product, error) {
	if s.getProductFn != nil {
		return s.getProductFn(ctx, productID)
	}
	return nil, nil
}

func (s stubSupplier) GetShippingEstimate(ctx context.Context, productID, country string) (*ShippingEstimate, error) {
	if s.getShippingFn != nil {
		return s.getShippingFn(ctx, productID, country)
	}
	return nil, nil
}

func (s stubSupplier) PlaceOrder(ctx context.Context, order OrderRequest) (*OrderConfirmation, error) {
	if s.placeOrderFn != nil {
		return s.placeOrderFn(ctx, order)
	}
	return nil, nil
}

func (s stubSupplier) GetOrder(ctx context.Context, orderID string) (*OrderStatus, error) {
	if s.getOrderFn != nil {
		return s.getOrderFn(ctx, orderID)
	}
	return nil, nil
}

func (s stubSupplier) GetTracking(ctx context.Context, orderID string) (*TrackingInfo, error) {
	if s.getTrackingFn != nil {
		return s.getTrackingFn(ctx, orderID)
	}
	return nil, nil
}

func TestSupplierSearchProductsToolAddsDataQualityNotes(t *testing.T) {
	tools := NewSupplierProductTools(stubSupplier{
		searchProductsFn: func(context.Context, string, SearchOpts) ([]Product, error) {
			return []Product{
				{ID: "p1", Price: 0, EstDeliveryDays: 0, Rating: -1},
				{ID: "p2", Price: 12.5, EstDeliveryDays: 5, Rating: -1},
			}, nil
		},
	})

	result, err := tools.SupplierSearchProductsTool(context.Background(), SearchProductsInput{
		Query: "water filter",
		Page:  3,
	})
	if err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success result, got error=%q", result.Error)
	}

	content, ok := result.Content.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result.Content)
	}
	if got, ok := content["count"].(int); !ok || got != 2 {
		t.Fatalf("expected count=2, got %#v", content["count"])
	}
	notes, ok := content["data_quality_notes"].([]string)
	if !ok {
		t.Fatalf("expected data_quality_notes, got %#v", content["data_quality_notes"])
	}
	if len(notes) == 0 {
		t.Fatalf("expected non-empty data quality notes")
	}
}

func TestSupplierSearchProductsToolReturnsErrorResultOnClientFailure(t *testing.T) {
	tools := NewSupplierProductTools(stubSupplier{
		searchProductsFn: func(context.Context, string, SearchOpts) ([]Product, error) {
			return nil, errors.New("upstream unavailable")
		},
	})

	result, err := tools.SupplierSearchProductsTool(context.Background(), SearchProductsInput{Query: "x"})
	if err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if result.Success {
		t.Fatalf("expected tool error result")
	}
	if !strings.Contains(result.Error, "supplier search failed") {
		t.Fatalf("unexpected error message: %q", result.Error)
	}
}

func TestSupplierPlaceOrderToolValidatesQuantity(t *testing.T) {
	called := false
	tools := NewSupplierOrderTools(stubSupplier{
		placeOrderFn: func(context.Context, OrderRequest) (*OrderConfirmation, error) {
			called = true
			return &OrderConfirmation{OrderID: "o-1"}, nil
		},
	})

	result, err := tools.SupplierPlaceOrderTool(context.Background(), PlaceOrderInput{
		ProductID: "p1",
		VariantID: "v1",
		Quantity:  0,
	})
	if err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if result.Success {
		t.Fatalf("expected validation failure result")
	}
	if called {
		t.Fatalf("expected client not to be called for invalid quantity")
	}
}

func TestSupplierCancelOrderToolStatusHandling(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		wantOK     bool
		wantMarker string
	}{
		{name: "shipped_denied", status: "shipped", wantOK: false, wantMarker: "cannot cancel order"},
		{name: "already_cancelled", status: "cancelled", wantOK: true, wantMarker: "already_cancelled"},
		{name: "pending_requests_cancel", status: "pending", wantOK: true, wantMarker: "cancel_requested"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tools := NewSupplierOrderTools(stubSupplier{
				getOrderFn: func(context.Context, string) (*OrderStatus, error) {
					return &OrderStatus{OrderID: "ord-1", Status: tc.status}, nil
				},
			})

			result, err := tools.SupplierCancelOrderTool(context.Background(), CancelOrderInput{OrderID: "ord-1"})
			if err != nil {
				t.Fatalf("unexpected tool error: %v", err)
			}
			if result.Success != tc.wantOK {
				t.Fatalf("unexpected success=%v for status %s", result.Success, tc.status)
			}
			if tc.wantOK {
				payload, ok := result.Content.(map[string]any)
				if !ok {
					t.Fatalf("unexpected success payload: %T", result.Content)
				}
				if !strings.Contains(payload["status"].(string), tc.wantMarker) {
					t.Fatalf("unexpected status payload: %#v", payload["status"])
				}
				return
			}
			if !strings.Contains(result.Error, tc.wantMarker) {
				t.Fatalf("unexpected error payload: %q", result.Error)
			}
		})
	}
}
