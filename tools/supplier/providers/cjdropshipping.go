package providers

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/original-david-knight/go_wild/tools/supplier/providers/cjdropshipping"
	"github.com/original-david-knight/go_wild/tools/supplier"
)

const (
	defaultCJDropshippingCurrency       = "USD"
	defaultCJDropshippingFromCountry    = "CN"
	defaultCJDropshippingSearchPageSize = 20
)

// CJDropshipping implements supplier.Supplier for CJ Dropshipping.
type CJDropshipping struct {
	client             *cjdropshipping.Client
	defaultFromCountry string
}

// NewCJDropshipping creates a new CJ Dropshipping supplier client.
func NewCJDropshipping(accessToken, platformToken, defaultFromCountry string) *CJDropshipping {
	country := strings.ToUpper(strings.TrimSpace(defaultFromCountry))
	if country == "" {
		country = defaultCJDropshippingFromCountry
	}
	client := cjdropshipping.NewClient(strings.TrimSpace(accessToken), cjdropshipping.WithPlatformToken(strings.TrimSpace(platformToken)))
	return &CJDropshipping{client: client, defaultFromCountry: country}
}

func mapCjSort(sortBy string) (orderBy int, sort string) {
	switch strings.TrimSpace(sortBy) {
	case "price_asc":
		return 2, "asc"
	case "price_desc":
		return 2, "desc"
	case "rating":
		return 1, "desc"
	case "orders":
		return 1, "desc"
	default:
		return 0, ""
	}
}

// SearchProducts searches the CJ catalog.
func (c *CJDropshipping) SearchProducts(ctx context.Context, query string, opts supplier.SearchOpts) ([]supplier.Product, error) {
	params := cjdropshipping.ProductListV2Params{
		KeyWord: query,
		Page:    opts.Page,
		Size:    defaultCJDropshippingSearchPageSize,
	}
	if params.Page <= 0 {
		params.Page = 1
	}
	if opts.Category != "" {
		params.CategoryID = strings.TrimSpace(opts.Category)
	}
	if opts.MinPrice > 0 {
		params.StartSellPrice = opts.MinPrice
	}
	if opts.MaxPrice > 0 {
		params.EndSellPrice = opts.MaxPrice
	}
	if opts.ShipsFromCountry != "" {
		params.CountryCode = strings.ToUpper(strings.TrimSpace(opts.ShipsFromCountry))
	}
	params.OrderBy, params.Sort = mapCjSort(opts.SortBy)

	resp, err := c.client.ListProductsV2(ctx, params)
	if err != nil {
		return nil, err
	}

	products := make([]supplier.Product, 0)
	for _, group := range resp.Content {
		for _, item := range group.ProductList {
			price := parsePriceString(item.SellPrice)
			if price <= 0 {
				price = parsePriceString(item.NowPrice)
			}
			if price <= 0 {
				price = parsePriceString(item.DiscountPrice)
			}

			deliveryDays := parseDeliveryMaxDays(item.DeliveryCycle)

			shipsFrom := strings.ToUpper(strings.TrimSpace(opts.ShipsFromCountry))
			if shipsFrom == "" {
				shipsFrom = c.defaultFromCountry
			}

			product := supplier.Product{
				ID:              strings.TrimSpace(item.ID),
				Title:           firstNonEmpty(item.NameEn, item.SKU),
				Description:     strings.TrimSpace(item.Description),
				Category:        firstNonEmpty(item.CategoryName, strings.TrimSpace(opts.Category)),
				Price:           price,
				Currency:        firstNonEmpty(strings.TrimSpace(item.Currency), defaultCJDropshippingCurrency),
				Rating:          -1,
				ReviewCount:     -1,
				ImageURLs:       nonEmptyStrings([]string{item.BigImage}),
				ShipsFrom:       shipsFrom,
				EstDeliveryDays: deliveryDays,
				SupplierName:    "cjdropshipping",
			}

			if opts.MinRating > 0 && product.Rating >= 0 && product.Rating < opts.MinRating {
				continue
			}
			if opts.MaxDeliveryDays > 0 && product.EstDeliveryDays > 0 && product.EstDeliveryDays > opts.MaxDeliveryDays {
				continue
			}
			products = append(products, product)
		}
	}

	// CJ's listV2 often returns empty prices. Enrich from variant data.
	c.enrichProductPricing(ctx, products)

	return products, nil
}

const enrichConcurrency = 5

// enrichProductPricing fetches variant data for products missing prices and
// fills in Price and ShipsFrom from the first variant.
func (c *CJDropshipping) enrichProductPricing(ctx context.Context, products []supplier.Product) {
	// Collect indices that need enrichment.
	var needEnrich []int
	for i := range products {
		if products[i].Price <= 0 {
			needEnrich = append(needEnrich, i)
		}
	}
	if len(needEnrich) == 0 {
		return
	}

	type enrichResult struct {
		idx      int
		price    float64
		shipsFrom string
	}

	var mu sync.Mutex
	var results []enrichResult
	sem := make(chan struct{}, enrichConcurrency)
	var wg sync.WaitGroup

	for _, idx := range needEnrich {
		pid := products[idx].ID
		if pid == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			variants, err := c.client.GetVariants(ctx, cjdropshipping.VariantQueryParams{PID: pid})
			if err != nil || len(variants) == 0 {
				return
			}

			// Use the lowest non-zero variant sell price.
			var bestPrice float64
			var bestCountry string
			for _, v := range variants {
				vPrice := v.VariantSellPrice.Float64()
				if vPrice > 0 && (bestPrice <= 0 || vPrice < bestPrice) {
					bestPrice = vPrice
				}
				if bestCountry == "" {
					if country := bestInventoryCountry(v.Inventories); country != "" {
						bestCountry = country
					}
				}
			}

			if bestPrice > 0 || bestCountry != "" {
				mu.Lock()
				results = append(results, enrichResult{idx: idx, price: bestPrice, shipsFrom: bestCountry})
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	for _, r := range results {
		if r.price > 0 {
			products[r.idx].Price = r.price
		}
		if r.shipsFrom != "" && products[r.idx].ShipsFrom == "" {
			products[r.idx].ShipsFrom = r.shipsFrom
		}
	}
}

// GetProduct retrieves a single CJ product by ID.
func (c *CJDropshipping) GetProduct(ctx context.Context, productID string) (*supplier.Product, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return nil, fmt.Errorf("product id is required")
	}

	detail, err := c.client.GetProductDetail(ctx, productID)
	if err != nil {
		return nil, err
	}

	variants, err := c.client.GetVariants(ctx, cjdropshipping.VariantQueryParams{PID: productID})
	if err != nil {
		return nil, err
	}

	mappedVariants := make([]supplier.Variant, 0, len(variants))
	for _, v := range variants {
		stockCount, _ := totalVariantStock(v)
		mappedVariants = append(mappedVariants, supplier.Variant{
			ID:         strings.TrimSpace(v.VID),
			Title:      firstNonEmpty(v.VariantNameEn, v.VariantName, v.VariantKey),
			Price:      v.VariantSellPrice.Float64(),
			SKU:        strings.TrimSpace(v.VariantSku),
			InStock:    stockCount > 0 || len(v.Inventories) == 0,
			StockCount: stockCount,
		})
	}

	price := parsePriceString(detail.SellPrice)
	if price <= 0 && len(mappedVariants) > 0 {
		price = mappedVariants[0].Price
	}

	imageURLs := nonEmptyStrings([]string{detail.ProductImage, detail.BigImage})

	product := &supplier.Product{
		ID:              productID,
		Title:           firstNonEmpty(detail.ProductNameEn, detail.NameEn, detail.ProductName),
		Description:     strings.TrimSpace(detail.Description),
		Category:        strings.TrimSpace(detail.CategoryName),
		Price:           price,
		Currency:        defaultCJDropshippingCurrency,
		Rating:          -1,
		ReviewCount:     -1,
		ImageURLs:       imageURLs,
		Variants:        mappedVariants,
		ShipsFrom:       c.defaultFromCountry,
		EstDeliveryDays: parseDeliveryMaxDays(detail.DeliveryCycle),
		SupplierName:    "cjdropshipping",
	}

	return product, nil
}

// GetShippingEstimate retrieves a shipping estimate for a product to a destination country.
func (c *CJDropshipping) GetShippingEstimate(ctx context.Context, productID, country string) (*supplier.ShippingEstimate, error) {
	productID = strings.TrimSpace(productID)
	country = strings.ToUpper(strings.TrimSpace(country))
	if productID == "" {
		return nil, fmt.Errorf("product id is required")
	}
	if country == "" {
		return nil, fmt.Errorf("country is required")
	}

	variants, err := c.client.GetVariants(ctx, cjdropshipping.VariantQueryParams{PID: productID})
	if err != nil {
		return nil, err
	}
	if len(variants) == 0 {
		return nil, fmt.Errorf("no variants found for product %s", productID)
	}

	choice, err := c.chooseShipping(ctx, variants[0].VID, country, 1)
	if err != nil {
		return nil, err
	}
	if choice == nil {
		return nil, fmt.Errorf("no shipping options available")
	}

	minDays, maxDays := parseDeliveryRange(choice.option.LogisticAging)
	cost := choice.option.TotalPostageFee.Float64()
	if cost <= 0 {
		cost = choice.option.LogisticPrice.Float64()
	}

	return &supplier.ShippingEstimate{
		ProductID:     productID,
		Country:       country,
		Method:        strings.TrimSpace(choice.option.LogisticName),
		Cost:          cost,
		Currency:      defaultCJDropshippingCurrency,
		MinDays:       minDays,
		MaxDays:       maxDays,
		TrackingAvail: true,
	}, nil
}

// PlaceOrder creates a CJ order for the given request.
func (c *CJDropshipping) PlaceOrder(ctx context.Context, order supplier.OrderRequest) (*supplier.OrderConfirmation, error) {
	if strings.TrimSpace(order.VariantID) == "" {
		return nil, fmt.Errorf("variant_id is required for cjdropshipping orders")
	}
	if order.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be greater than 0")
	}
	countryCode := strings.ToUpper(strings.TrimSpace(order.ShippingCountry))
	if countryCode == "" {
		return nil, fmt.Errorf("shipping_country is required")
	}

	choice, err := c.chooseShipping(ctx, order.VariantID, countryCode, order.Quantity)
	if err != nil {
		return nil, err
	}
	if choice == nil {
		return nil, fmt.Errorf("no shipping options available for variant %s", order.VariantID)
	}

	orderNumber := strings.TrimSpace(order.ShopifyOrderID)
	if orderNumber == "" {
		orderNumber = fmt.Sprintf("gowild-%d", time.Now().UnixNano())
	}

	payload := cjdropshipping.CreateOrderV3Request{
		OrderNumber:          orderNumber,
		ShippingZip:          strings.TrimSpace(order.ShippingZip),
		ShippingCountryCode:  countryCode,
		ShippingCountry:      countryCode,
		ShippingProvince:     strings.TrimSpace(order.ShippingState),
		ShippingCity:         strings.TrimSpace(order.ShippingCity),
		ShippingPhone:        strings.TrimSpace(order.ShippingPhone),
		ShippingCustomerName: strings.TrimSpace(order.ShippingName),
		ShippingAddress:      strings.TrimSpace(order.ShippingAddress),
		LogisticName:         strings.TrimSpace(choice.option.LogisticName),
		FromCountryCode:      strings.TrimSpace(choice.fromCountry),
		ShopLogisticsType:    2,
		Products: []cjdropshipping.CreateOrderV3Product{
			{
				VID:      strings.TrimSpace(order.VariantID),
				Quantity: order.Quantity,
			},
		},
	}

	resp, err := c.client.CreateOrderV3(ctx, payload)
	if err != nil {
		return nil, err
	}

	minDays, maxDays := parseDeliveryRange(choice.option.LogisticAging)
	confirmation := &supplier.OrderConfirmation{
		OrderID:        firstNonEmpty(strings.TrimSpace(resp.OrderID), strings.TrimSpace(resp.OrderNumber)),
		Status:         strings.ToLower(strings.TrimSpace(resp.OrderStatus)),
		Total:          resp.OrderAmount.Float64(),
		Currency:       defaultCJDropshippingCurrency,
		EstDeliveryMin: minDays,
		EstDeliveryMax: maxDays,
	}
	return confirmation, nil
}

// GetOrder retrieves order details.
func (c *CJDropshipping) GetOrder(ctx context.Context, orderID string) (*supplier.OrderStatus, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, fmt.Errorf("order id is required")
	}

	detail, err := c.client.GetOrderDetail(ctx, orderID, nil)
	if err != nil {
		return nil, err
	}

	return &supplier.OrderStatus{
		OrderID:        firstNonEmpty(strings.TrimSpace(detail.OrderID), orderID),
		Status:         strings.ToLower(strings.TrimSpace(detail.OrderStatus)),
		Total:          detail.OrderAmount.Float64(),
		Currency:       defaultCJDropshippingCurrency,
		TrackingNumber: strings.TrimSpace(detail.TrackNumber),
		TrackingURL:    strings.TrimSpace(detail.TrackingURL),
		ShippedAt:      strings.TrimSpace(detail.OutWarehouseTime),
		DeliveredAt:    "",
	}, nil
}

// GetTracking retrieves tracking information for an order.
func (c *CJDropshipping) GetTracking(ctx context.Context, orderID string) (*supplier.TrackingInfo, error) {
	status, err := c.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}

	trackNumber := strings.TrimSpace(status.TrackingNumber)
	if trackNumber == "" {
		return &supplier.TrackingInfo{
			OrderID:        status.OrderID,
			TrackingNumber: "",
			Carrier:        strings.TrimSpace(status.TrackingURL),
			Status:         "pending",
			TrackingURL:    strings.TrimSpace(status.TrackingURL),
		}, nil
	}

	trackInfo, err := c.client.GetTrackInfo(ctx, trackNumber)
	if err != nil {
		return nil, err
	}

	return &supplier.TrackingInfo{
		OrderID:        status.OrderID,
		TrackingNumber: strings.TrimSpace(trackInfo.TrackingNumber),
		Carrier:        strings.TrimSpace(trackInfo.LogisticName),
		Status:         normalizeTrackingStatus(trackInfo.TrackingStatus),
		TrackingURL:    strings.TrimSpace(status.TrackingURL),
		EstDelivery:    strings.TrimSpace(trackInfo.DeliveryTime),
	}, nil
}

type shippingChoice struct {
	fromCountry string
	option      cjdropshipping.FreightOption
}

func (c *CJDropshipping) chooseShipping(ctx context.Context, vid, destCountry string, quantity int) (*shippingChoice, error) {
	vid = strings.TrimSpace(vid)
	if vid == "" {
		return nil, fmt.Errorf("variant id is required")
	}
	if quantity <= 0 {
		quantity = 1
	}

	fromCountry, err := c.resolveFromCountry(ctx, vid)
	if err != nil {
		return nil, err
	}

	opts, err := c.client.FreightCalculate(ctx, cjdropshipping.FreightCalculateRequest{
		StartCountryCode: fromCountry,
		EndCountryCode:   strings.ToUpper(strings.TrimSpace(destCountry)),
		Products: []cjdropshipping.FreightProduct{{
			Quantity: quantity,
			VID:      vid,
		}},
	})
	if err != nil {
		return nil, err
	}
	if len(opts) == 0 {
		return nil, nil
	}

	sort.Slice(opts, func(i, j int) bool {
		return shippingOptionCost(opts[i]) < shippingOptionCost(opts[j])
	})

	return &shippingChoice{fromCountry: fromCountry, option: opts[0]}, nil
}

func (c *CJDropshipping) resolveFromCountry(ctx context.Context, vid string) (string, error) {
	variant, err := c.client.GetVariantByID(ctx, vid)
	if err == nil && variant != nil {
		if country := bestInventoryCountry(variant.Inventories); country != "" {
			return country, nil
		}
	}

	stockRows, err := c.client.GetStockByVID(ctx, vid)
	if err == nil {
		bestCountry := ""
		bestTotal := -1
		for _, row := range stockRows {
			total := row.TotalInventoryNum.Int()
			if total <= 0 {
				total = row.StorageNum.Int()
			}
			if total > bestTotal {
				bestTotal = total
				bestCountry = strings.ToUpper(strings.TrimSpace(row.CountryCode))
			}
		}
		if bestCountry != "" {
			return bestCountry, nil
		}
	}

	if c.defaultFromCountry != "" {
		return c.defaultFromCountry, nil
	}
	return defaultCJDropshippingFromCountry, nil
}

func totalVariantStock(v cjdropshipping.Variant) (int, string) {
	bestCountry := ""
	total := 0
	bestCountryTotal := -1
	for _, inv := range v.Inventories {
		invTotal := inv.TotalInventory.Int()
		total += invTotal
		if invTotal > bestCountryTotal {
			bestCountryTotal = invTotal
			bestCountry = strings.ToUpper(strings.TrimSpace(inv.CountryCode))
		}
	}
	return total, bestCountry
}

func bestInventoryCountry(inventories []cjdropshipping.VariantInventory) string {
	bestCountry := ""
	bestTotal := -1
	for _, inv := range inventories {
		total := inv.TotalInventory.Int()
		if total > bestTotal {
			bestTotal = total
			bestCountry = strings.ToUpper(strings.TrimSpace(inv.CountryCode))
		}
	}
	return bestCountry
}

func shippingOptionCost(opt cjdropshipping.FreightOption) float64 {
	if v := opt.TotalPostageFee.Float64(); v > 0 {
		return v
	}
	if v := opt.LogisticPrice.Float64(); v > 0 {
		return v
	}
	if v := opt.LogisticPriceCn.Float64(); v > 0 {
		return v
	}
	return math.MaxFloat64
}

func parsePriceString(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseDeliveryMaxDays(raw string) int {
	_, max := parseDeliveryRange(raw)
	return max
}

func parseDeliveryRange(raw string) (int, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0
	}
	digits := make([]int, 0, 2)
	current := strings.Builder{}
	flush := func() {
		if current.Len() == 0 {
			return
		}
		if n, err := strconv.Atoi(current.String()); err == nil {
			digits = append(digits, n)
		}
		current.Reset()
	}
	for _, ch := range raw {
		if ch >= '0' && ch <= '9' {
			current.WriteRune(ch)
			continue
		}
		flush()
	}
	flush()

	switch len(digits) {
	case 0:
		return 0, 0
	case 1:
		return digits[0], digits[0]
	default:
		minDays := digits[0]
		maxDays := digits[1]
		if maxDays < minDays {
			minDays, maxDays = maxDays, minDays
		}
		return minDays, maxDays
	}
}

func normalizeTrackingStatus(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch {
	case raw == "":
		return "unknown"
	case strings.Contains(raw, "delivered"):
		return "delivered"
	case strings.Contains(raw, "transit"):
		return "in_transit"
	case strings.Contains(raw, "out for"):
		return "out_for_delivery"
	case strings.Contains(raw, "exception"):
		return "exception"
	default:
		return raw
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func nonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
