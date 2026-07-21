package providers

import "testing"

func TestMapCjSort(t *testing.T) {
	cases := []struct {
		name        string
		sortBy      string
		wantOrderBy int
		wantSort    string
	}{
		{name: "price_asc", sortBy: "price_asc", wantOrderBy: 2, wantSort: "asc"},
		{name: "price_desc", sortBy: "price_desc", wantOrderBy: 2, wantSort: "desc"},
		{name: "rating", sortBy: "rating", wantOrderBy: 1, wantSort: "desc"},
		{name: "unknown", sortBy: "unknown", wantOrderBy: 0, wantSort: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotOrderBy, gotSort := mapCjSort(tc.sortBy)
			if gotOrderBy != tc.wantOrderBy || gotSort != tc.wantSort {
				t.Fatalf("mapCjSort(%q) = (%d,%q), want (%d,%q)", tc.sortBy, gotOrderBy, gotSort, tc.wantOrderBy, tc.wantSort)
			}
		})
	}
}

func TestParseDeliveryRange(t *testing.T) {
	cases := []struct {
		raw     string
		wantMin int
		wantMax int
	}{
		{raw: "5-12 days", wantMin: 5, wantMax: 12},
		{raw: "12 to 5 days", wantMin: 5, wantMax: 12},
		{raw: "7days", wantMin: 7, wantMax: 7},
		{raw: "", wantMin: 0, wantMax: 0},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			gotMin, gotMax := parseDeliveryRange(tc.raw)
			if gotMin != tc.wantMin || gotMax != tc.wantMax {
				t.Fatalf("parseDeliveryRange(%q) = (%d,%d), want (%d,%d)", tc.raw, gotMin, gotMax, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestNormalizeTrackingStatus(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "", want: "unknown"},
		{raw: "Delivered to mailbox", want: "delivered"},
		{raw: "In transit to destination", want: "in_transit"},
		{raw: "Out for delivery", want: "out_for_delivery"},
		{raw: "Delivery exception", want: "exception"},
		{raw: "custom_status", want: "custom_status"},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			if got := normalizeTrackingStatus(tc.raw); got != tc.want {
				t.Fatalf("normalizeTrackingStatus(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNonEmptyStringsDeduplicatesAndTrims(t *testing.T) {
	got := nonEmptyStrings([]string{"  a  ", "", "b", "a", "  ", "b", "c"})
	if len(got) != 3 {
		t.Fatalf("expected 3 unique values, got %#v", got)
	}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("unexpected output ordering/content: %#v", got)
	}
}

func TestTopDawgToProductMapping(t *testing.T) {
	p := tdProduct{
		ID:              "p1",
		Title:           "Portable Light",
		Description:     "USB rechargeable",
		Category:        "Home",
		Price:           15.5,
		CompareAtPrice:  20.0,
		Rating:          4.3,
		ReviewCount:     128,
		Images:          []string{"https://img/1.jpg"},
		ShipsFrom:       "US",
		EstDeliveryDays: 6,
		Variants: []tdVariant{
			{ID: "v1", Title: "Black", Price: 15.5, SKU: "SKU-1", InStock: true, StockCount: 10},
		},
	}

	mapped := p.toProduct()
	if mapped.SupplierName != "topdawg" || mapped.Currency != "USD" {
		t.Fatalf("unexpected defaults in mapped product: %#v", mapped)
	}
	if len(mapped.Variants) != 1 || mapped.Variants[0].ID != "v1" {
		t.Fatalf("unexpected variant mapping: %#v", mapped.Variants)
	}
	if mapped.Title != "Portable Light" || mapped.Price != 15.5 {
		t.Fatalf("unexpected product mapping: %#v", mapped)
	}
}
