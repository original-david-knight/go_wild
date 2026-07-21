package cjdropshipping

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Decimal handles JSON numbers represented as either strings or numbers.
type Decimal float64

func (d *Decimal) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*d = 0
		return nil
	}
	if strings.HasPrefix(raw, "\"") {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		raw = strings.TrimSpace(s)
		if raw == "" {
			*d = 0
			return nil
		}
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return err
	}
	*d = Decimal(f)
	return nil
}

func (d Decimal) Float64() float64 {
	return float64(d)
}

// IntValue handles JSON integers represented as either strings or numbers.
type IntValue int

func (v *IntValue) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*v = 0
		return nil
	}
	if strings.HasPrefix(raw, "\"") {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		raw = strings.TrimSpace(s)
		if raw == "" {
			*v = 0
			return nil
		}
	}
	i, err := strconv.Atoi(raw)
	if err != nil {
		f, ferr := strconv.ParseFloat(raw, 64)
		if ferr != nil {
			return err
		}
		i = int(f)
	}
	*v = IntValue(i)
	return nil
}

func (v IntValue) Int() int {
	return int(v)
}

func parseCJTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

// TokenResponse is returned by getAccessToken/refreshAccessToken.
type TokenResponse struct {
	OpenID                 IntValue `json:"openId"`
	AccessToken            string   `json:"accessToken"`
	AccessTokenExpiryDate  string   `json:"accessTokenExpiryDate"`
	RefreshToken           string   `json:"refreshToken"`
	RefreshTokenExpiryDate string   `json:"refreshTokenExpiryDate"`
	CreateDate             string   `json:"createDate"`
}

func (t TokenResponse) AccessTokenExpiresAt() time.Time {
	return parseCJTime(t.AccessTokenExpiryDate)
}

func (t TokenResponse) RefreshTokenExpiresAt() time.Time {
	return parseCJTime(t.RefreshTokenExpiryDate)
}

// ProductListV2Params configures /product/listV2 queries.
type ProductListV2Params struct {
	KeyWord        string
	Page           int
	Size           int
	CategoryID     string
	CountryCode    string
	StartSellPrice float64
	EndSellPrice   float64
	Sort           string
	OrderBy        int
	Features       []string
	Currency       string
}

// ProductListV2Data is the data payload for /product/listV2.
type ProductListV2Data struct {
	PageSize     IntValue               `json:"pageSize"`
	PageNumber   IntValue               `json:"pageNumber"`
	TotalRecords IntValue               `json:"totalRecords"`
	TotalPages   IntValue               `json:"totalPages"`
	Content      []ProductListV2Content `json:"content"`
}

type ProductListV2Content struct {
	ProductList []ProductSummary `json:"productList"`
	KeyWord     string           `json:"keyWord"`
	KeyWordOld  string           `json:"keyWordOld"`
}

// ProductSummary is a product record from listV2.
type ProductSummary struct {
	ID                    string   `json:"id"`
	NameEn                string   `json:"nameEn"`
	SKU                   string   `json:"sku"`
	SPU                   string   `json:"spu"`
	BigImage              string   `json:"bigImage"`
	SellPrice             string   `json:"sellPrice"`
	NowPrice              string   `json:"nowPrice"`
	DiscountPrice         string   `json:"discountPrice"`
	CategoryID            string   `json:"categoryId"`
	CategoryName          string   `json:"threeCategoryName"`
	SupplierName          string   `json:"supplierName"`
	WarehouseInventoryNum IntValue `json:"warehouseInventoryNum"`
	DeliveryCycle         string   `json:"deliveryCycle"`
	Currency              string   `json:"currency"`
	Description           string   `json:"description"`
	ListedNum             IntValue `json:"listedNum"`
}

// ProductDetail is returned by /product/query.
type ProductDetail struct {
	PID           string `json:"pid"`
	ID            string `json:"id"`
	ProductName   string `json:"productName"`
	ProductNameEn string `json:"productNameEn"`
	NameEn        string `json:"nameEn"`
	ProductSku    string `json:"productSku"`
	ProductImage  string `json:"productImage"`
	BigImage      string `json:"bigImage"`
	Description   string `json:"description"`
	CategoryID    string `json:"categoryId"`
	CategoryName  string `json:"categoryName"`
	SellPrice     string `json:"sellPrice"`
	DeliveryCycle string `json:"deliveryCycle"`
}

// VariantQueryParams configures /product/variant/query.
type VariantQueryParams struct {
	PID         string
	ProductSKU  string
	VariantSKU  string
	CountryCode string
}

// Variant represents a CJ variant.
type Variant struct {
	VID                 string             `json:"vid"`
	PID                 string             `json:"pid"`
	VariantName         string             `json:"variantName"`
	VariantNameEn       string             `json:"variantNameEn"`
	VariantImage        string             `json:"variantImage"`
	VariantSku          string             `json:"variantSku"`
	VariantKey          string             `json:"variantKey"`
	VariantSellPrice    Decimal            `json:"variantSellPrice"`
	VariantSugSellPrice Decimal            `json:"variantSugSellPrice"`
	VariantWeight       IntValue           `json:"variantWeight"`
	Inventories         []VariantInventory `json:"inventories"`
}

type VariantInventory struct {
	CountryCode       string         `json:"countryCode"`
	TotalInventory    IntValue       `json:"totalInventory"`
	CjInventory       IntValue       `json:"cjInventory"`
	FactoryInventory  IntValue       `json:"factoryInventory"`
	VerifiedWarehouse string         `json:"verifiedWarehouse"`
	Stock             []VariantStock `json:"stock"`
}

type VariantStock struct {
	StockID          string   `json:"stockId"`
	Inventory        IntValue `json:"inventory"`
	FactoryInventory IntValue `json:"factoryInventory"`
}

// StockByVIDItem is returned by /product/stock/queryByVid.
type StockByVIDItem struct {
	VID                 string         `json:"vid"`
	AreaID              IntValue       `json:"areaId"`
	AreaEn              string         `json:"areaEn"`
	CountryCode         string         `json:"countryCode"`
	CountryNameEn       string         `json:"countryNameEn"`
	StorageNum          IntValue       `json:"storageNum"`
	TotalInventoryNum   IntValue       `json:"totalInventoryNum"`
	CjInventoryNum      IntValue       `json:"cjInventoryNum"`
	FactoryInventoryNum IntValue       `json:"factoryInventoryNum"`
	Stock               []VariantStock `json:"stock"`
}

// FreightCalculateRequest is the payload for /logistic/freightCalculate.
type FreightCalculateRequest struct {
	StartCountryCode string           `json:"startCountryCode"`
	EndCountryCode   string           `json:"endCountryCode"`
	Zip              string           `json:"zip,omitempty"`
	TaxID            string           `json:"taxId,omitempty"`
	HouseNumber      string           `json:"houseNumber,omitempty"`
	IOSSNumber       string           `json:"iossNumber,omitempty"`
	Products         []FreightProduct `json:"products"`
}

type FreightProduct struct {
	Quantity int    `json:"quantity"`
	VID      string `json:"vid"`
}

// FreightOption is one candidate shipping option.
type FreightOption struct {
	LogisticPrice         Decimal `json:"logisticPrice"`
	LogisticPriceCn       Decimal `json:"logisticPriceCn"`
	LogisticAging         string  `json:"logisticAging"`
	LogisticName          string  `json:"logisticName"`
	TaxesFee              Decimal `json:"taxesFee"`
	ClearanceOperationFee Decimal `json:"clearanceOperationFee"`
	TotalPostageFee       Decimal `json:"totalPostageFee"`
}

// TrackInfo is returned by /logistic/trackInfo.
type TrackInfo struct {
	TrackingNumber  string `json:"trackingNumber"`
	LogisticName    string `json:"logisticName"`
	TrackingFrom    string `json:"trackingFrom"`
	TrackingTo      string `json:"trackingTo"`
	DeliveryDay     string `json:"deliveryDay"`
	DeliveryTime    string `json:"deliveryTime"`
	TrackingStatus  string `json:"trackingStatus"`
	LastMileCarrier string `json:"lastMileCarrier"`
	LastTrackNumber string `json:"lastTrackNumber"`
}

// CreateOrderV3Request is the payload for /shopping/order/createOrderV3.
type CreateOrderV3Request struct {
	OrderNumber          string                 `json:"orderNumber"`
	ShippingZip          string                 `json:"shippingZip,omitempty"`
	ShippingCountryCode  string                 `json:"shippingCountryCode"`
	ShippingCountry      string                 `json:"shippingCountry"`
	ShippingProvince     string                 `json:"shippingProvince"`
	ShippingCity         string                 `json:"shippingCity"`
	ShippingCounty       string                 `json:"shippingCounty,omitempty"`
	ShippingPhone        string                 `json:"shippingPhone,omitempty"`
	ShippingCustomerName string                 `json:"shippingCustomerName"`
	ShippingAddress      string                 `json:"shippingAddress"`
	ShippingAddress2     string                 `json:"shippingAddress2,omitempty"`
	HouseNumber          string                 `json:"houseNumber,omitempty"`
	Email                string                 `json:"email,omitempty"`
	TaxID                string                 `json:"taxId,omitempty"`
	Remark               string                 `json:"remark,omitempty"`
	ConsigneeID          string                 `json:"consigneeID,omitempty"`
	ShopAmount           Decimal                `json:"shopAmount,omitempty"`
	LogisticName         string                 `json:"logisticName"`
	FromCountryCode      string                 `json:"fromCountryCode"`
	Platform             string                 `json:"platform,omitempty"`
	IOSSType             IntValue               `json:"iossType,omitempty"`
	IOSSNumber           string                 `json:"iossNumber,omitempty"`
	ShopLogisticsType    IntValue               `json:"shopLogisticsType,omitempty"`
	StorageID            string                 `json:"storageId,omitempty"`
	Products             []CreateOrderV3Product `json:"products"`
}

type CreateOrderV3Product struct {
	VID             string  `json:"vid,omitempty"`
	SKU             string  `json:"sku,omitempty"`
	Quantity        int     `json:"quantity"`
	UnitPrice       Decimal `json:"unitPrice,omitempty"`
	StoreLineItemID string  `json:"storeLineItemId,omitempty"`
	PODProperties   string  `json:"podProperties,omitempty"`
}

// CreateOrderV3Response is the data payload returned by createOrderV3.
type CreateOrderV3Response struct {
	OrderID            string  `json:"orderId"`
	OrderNumber        string  `json:"orderNumber"`
	ShipmentOrderID    string  `json:"shipmentOrderId"`
	IOSSAmount         Decimal `json:"iossAmount"`
	IOSSTaxHandlingFee Decimal `json:"iossTaxHandlingFee"`
	PostageAmount      Decimal `json:"postageAmount"`
	ProductAmount      Decimal `json:"productAmount"`
	ActualPayment      Decimal `json:"actualPayment"`
	OrderAmount        Decimal `json:"orderAmount"`
	OrderStatus        string  `json:"orderStatus"`
	CjPayURL           string  `json:"cjPayUrl"`
}

// OrderDetail is returned by /shopping/order/getOrderDetail.
type OrderDetail struct {
	OrderID              string               `json:"orderId"`
	OrderNum             string               `json:"orderNum"`
	PlatformOrderID      string               `json:"platformOrderId"`
	CJOrderID            string               `json:"cjOrderId"`
	CJOrderCode          string               `json:"cjOrderCode"`
	FromCountryCode      string               `json:"fromCountryCode"`
	ShippingCountryCode  string               `json:"shippingCountryCode"`
	ShippingProvince     string               `json:"shippingProvince"`
	ShippingCity         string               `json:"shippingCity"`
	ShippingAddress      string               `json:"shippingAddress"`
	ShippingCustomerName string               `json:"shippingCustomerName"`
	ShippingPhone        string               `json:"shippingPhone"`
	Remark               string               `json:"remark"`
	LogisticName         string               `json:"logisticName"`
	TrackNumber          string               `json:"trackNumber"`
	TrackingURL          string               `json:"trackingUrl"`
	OrderWeight          IntValue             `json:"orderWeight"`
	OrderAmount          Decimal              `json:"orderAmount"`
	OrderStatus          string               `json:"orderStatus"`
	CreateDate           string               `json:"createDate"`
	PaymentDate          string               `json:"paymentDate"`
	OutWarehouseTime     string               `json:"outWarehouseTime"`
	StoreCreateDate      string               `json:"storeCreateDate"`
	ProductAmount        Decimal              `json:"productAmount"`
	PostageAmount        Decimal              `json:"postageAmount"`
	ProductList          []OrderDetailProduct `json:"productList"`
}

type OrderDetailProduct struct {
	VID             string   `json:"vid"`
	Quantity        IntValue `json:"quantity"`
	SellPrice       Decimal  `json:"sellPrice"`
	StoreLineItemID string   `json:"storeLineItemId"`
	LineItemID      string   `json:"lineItemId"`
}

// ListOrdersParams configures /shopping/order/list.
type ListOrdersParams struct {
	PageNum         int
	PageSize        int
	OrderIDs        []string
	ShipmentOrderID string
	Status          string
}

// ListOrdersResponse is returned by /shopping/order/list.
type ListOrdersResponse struct {
	PageNum  IntValue     `json:"pageNum"`
	PageSize IntValue     `json:"pageSize"`
	Total    IntValue     `json:"total"`
	List     []OrderBrief `json:"list"`
}

type OrderBrief struct {
	OrderID              string   `json:"orderId"`
	OrderNum             string   `json:"orderNum"`
	CJOrderID            string   `json:"cjOrderId"`
	ShippingCountryCode  string   `json:"shippingCountryCode"`
	ShippingProvince     string   `json:"shippingProvince"`
	ShippingCity         string   `json:"shippingCity"`
	ShippingAddress      string   `json:"shippingAddress"`
	ShippingCustomerName string   `json:"shippingCustomerName"`
	ShippingPhone        string   `json:"shippingPhone"`
	Remark               string   `json:"remark"`
	LogisticName         string   `json:"logisticName"`
	TrackNumber          string   `json:"trackNumber"`
	TrackingURL          string   `json:"trackingUrl"`
	OrderWeight          IntValue `json:"orderWeight"`
	OrderAmount          Decimal  `json:"orderAmount"`
	OrderStatus          string   `json:"orderStatus"`
	CreateDate           string   `json:"createDate"`
	PaymentDate          string   `json:"paymentDate"`
	StoreCreateDate      string   `json:"storeCreateDate"`
	ProductAmount        Decimal  `json:"productAmount"`
	PostageAmount        Decimal  `json:"postageAmount"`
}
