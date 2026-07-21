package cjdropshipping

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	gowild_my "github.com/original-david-knight/go_wild/my"
)

// globalQPS enforces a process-wide rate limit across all CJ API calls,
// regardless of how many Client instances exist.
// ~0.77 QPS = 1.3 s between requests (CJ rejects at 1 QPS).
var globalQPS = gowild_my.NewQPSLimiter(0.77)

// Client is a CJ Dropshipping API client.
type Client struct {
	httpClient    *http.Client
	baseURL       string
	accessToken   string
	platformToken string
}

// APIError is a structured API/client error.
type APIError struct {
	StatusCode int
	Code       int
	Message    string
	RequestID  string
	Body       string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != 0 {
		return fmt.Sprintf("cj api error code=%d status=%d: %s", e.Code, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("cj api error status=%d: %s", e.StatusCode, e.Message)
}

// NewClient creates a client with an optional access token.
func NewClient(accessToken string, opts ...Option) *Client {
	c := &Client{
		httpClient:  &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:     DefaultBaseURL,
		accessToken: strings.TrimSpace(accessToken),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

func (c *Client) SetAccessToken(token string) {
	c.accessToken = strings.TrimSpace(token)
}

func (c *Client) AccessToken() string {
	return strings.TrimSpace(c.accessToken)
}

func (c *Client) SetPlatformToken(token string) {
	c.platformToken = strings.TrimSpace(token)
}

type apiEnvelope struct {
	Code      int             `json:"code"`
	Result    bool            `json:"result"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"requestId"`
}

func (c *Client) doRequestData(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body any,
	requireAccessToken bool,
	includePlatformToken bool,
) (json.RawMessage, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	if err := globalQPS.Wait(ctx); err != nil {
		return nil, err
	}
	if requireAccessToken && strings.TrimSpace(c.accessToken) == "" {
		return nil, fmt.Errorf("access token is required")
	}

	u := strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if requireAccessToken || strings.TrimSpace(c.accessToken) != "" {
		req.Header.Set("CJ-Access-Token", strings.TrimSpace(c.accessToken))
	}
	if includePlatformToken && strings.TrimSpace(c.platformToken) != "" {
		req.Header.Set("platformToken", strings.TrimSpace(c.platformToken))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var env apiEnvelope
	hasEnvelope := json.Unmarshal(respBody, &env) == nil && (env.Code != 0 || env.Message != "" || env.RequestID != "" || len(env.Data) > 0)

	if resp.StatusCode >= 400 {
		if hasEnvelope {
			msg := strings.TrimSpace(env.Message)
			if msg == "" {
				msg = strings.TrimSpace(string(respBody))
			}
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Code:       env.Code,
				Message:    msg,
				RequestID:  env.RequestID,
				Body:       string(respBody),
			}
		}
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(respBody)),
			Body:       string(respBody),
		}
	}

	if hasEnvelope {
		if env.Code != 200 || !env.Result {
			msg := strings.TrimSpace(env.Message)
			if msg == "" {
				msg = "request failed"
			}
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Code:       env.Code,
				Message:    msg,
				RequestID:  env.RequestID,
				Body:       string(respBody),
			}
		}
		if len(env.Data) == 0 || string(env.Data) == "null" {
			return nil, nil
		}
		return env.Data, nil
	}

	if len(respBody) == 0 {
		return nil, nil
	}
	return json.RawMessage(respBody), nil
}

func unmarshalData[T any](data json.RawMessage) (*T, error) {
	if len(data) == 0 || string(data) == "null" {
		var zero T
		return &zero, nil
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func decodeList[T any](data json.RawMessage) ([]T, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var direct []T
	if err := json.Unmarshal(data, &direct); err == nil {
		return direct, nil
	}

	var wrapped struct {
		List        []T `json:"list"`
		Content     []T `json:"content"`
		Inventories []T `json:"inventories"`
		Data        []T `json:"data"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil {
		switch {
		case len(wrapped.List) > 0:
			return wrapped.List, nil
		case len(wrapped.Content) > 0:
			return wrapped.Content, nil
		case len(wrapped.Inventories) > 0:
			return wrapped.Inventories, nil
		case len(wrapped.Data) > 0:
			return wrapped.Data, nil
		default:
			return []T{}, nil
		}
	}

	return nil, fmt.Errorf("unsupported list payload shape")
}

// GetAccessToken calls /authentication/getAccessToken.
func (c *Client) GetAccessToken(ctx context.Context, apiKey string) (*TokenResponse, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}
	data, err := c.doRequestData(ctx, http.MethodPost, "/authentication/getAccessToken", nil, map[string]string{
		"apiKey": apiKey,
	}, false, false)
	if err != nil {
		return nil, err
	}
	return unmarshalData[TokenResponse](data)
}

// RefreshAccessToken calls /authentication/refreshAccessToken.
func (c *Client) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh token is required")
	}
	data, err := c.doRequestData(ctx, http.MethodPost, "/authentication/refreshAccessToken", nil, map[string]string{
		"refreshToken": refreshToken,
	}, false, false)
	if err != nil {
		return nil, err
	}
	return unmarshalData[TokenResponse](data)
}

// ListProductsV2 calls /product/listV2.
func (c *Client) ListProductsV2(ctx context.Context, params ProductListV2Params) (*ProductListV2Data, error) {
	q := url.Values{}
	if v := strings.TrimSpace(params.KeyWord); v != "" {
		q.Set("keyWord", v)
	}
	if params.Page > 0 {
		q.Set("page", strconv.Itoa(params.Page))
	}
	if params.Size > 0 {
		q.Set("size", strconv.Itoa(params.Size))
	}
	if v := strings.TrimSpace(params.CategoryID); v != "" {
		q.Set("categoryId", v)
	}
	if v := strings.TrimSpace(params.CountryCode); v != "" {
		q.Set("countryCode", strings.ToUpper(v))
	}
	if params.StartSellPrice > 0 {
		q.Set("startSellPrice", strconv.FormatFloat(params.StartSellPrice, 'f', -1, 64))
	}
	if params.EndSellPrice > 0 {
		q.Set("endSellPrice", strconv.FormatFloat(params.EndSellPrice, 'f', -1, 64))
	}
	if v := strings.TrimSpace(params.Sort); v != "" {
		q.Set("sort", v)
	}
	if params.OrderBy > 0 {
		q.Set("orderBy", strconv.Itoa(params.OrderBy))
	}
	if v := strings.TrimSpace(params.Currency); v != "" {
		q.Set("currency", strings.ToUpper(v))
	}
	for _, feature := range params.Features {
		if f := strings.TrimSpace(feature); f != "" {
			q.Add("features", f)
		}
	}

	data, err := c.doRequestData(ctx, http.MethodGet, "/product/listV2", q, nil, true, false)
	if err != nil {
		return nil, err
	}
	return unmarshalData[ProductListV2Data](data)
}

// GetProductDetail calls /product/query.
func (c *Client) GetProductDetail(ctx context.Context, pid string) (*ProductDetail, error) {
	pid = strings.TrimSpace(pid)
	if pid == "" {
		return nil, fmt.Errorf("pid is required")
	}
	q := url.Values{}
	q.Set("pid", pid)
	data, err := c.doRequestData(ctx, http.MethodGet, "/product/query", q, nil, true, false)
	if err != nil {
		return nil, err
	}
	return unmarshalData[ProductDetail](data)
}

// GetVariants calls /product/variant/query.
func (c *Client) GetVariants(ctx context.Context, params VariantQueryParams) ([]Variant, error) {
	q := url.Values{}
	if v := strings.TrimSpace(params.PID); v != "" {
		q.Set("pid", v)
	}
	if v := strings.TrimSpace(params.ProductSKU); v != "" {
		q.Set("productSku", v)
	}
	if v := strings.TrimSpace(params.VariantSKU); v != "" {
		q.Set("variantSku", v)
	}
	if v := strings.TrimSpace(params.CountryCode); v != "" {
		q.Set("countryCode", strings.ToUpper(v))
	}
	if len(q) == 0 {
		return nil, fmt.Errorf("at least one variant query field is required")
	}
	data, err := c.doRequestData(ctx, http.MethodGet, "/product/variant/query", q, nil, true, false)
	if err != nil {
		return nil, err
	}
	return decodeList[Variant](data)
}

// GetVariantByID calls /product/variant/queryByVid.
func (c *Client) GetVariantByID(ctx context.Context, vid string) (*Variant, error) {
	vid = strings.TrimSpace(vid)
	if vid == "" {
		return nil, fmt.Errorf("vid is required")
	}
	q := url.Values{}
	q.Set("vid", vid)
	data, err := c.doRequestData(ctx, http.MethodGet, "/product/variant/queryByVid", q, nil, true, false)
	if err != nil {
		return nil, err
	}
	return unmarshalData[Variant](data)
}

// GetStockByVID calls /product/stock/queryByVid.
func (c *Client) GetStockByVID(ctx context.Context, vid string) ([]StockByVIDItem, error) {
	vid = strings.TrimSpace(vid)
	if vid == "" {
		return nil, fmt.Errorf("vid is required")
	}
	q := url.Values{}
	q.Set("vid", vid)
	data, err := c.doRequestData(ctx, http.MethodGet, "/product/stock/queryByVid", q, nil, true, false)
	if err != nil {
		return nil, err
	}
	return decodeList[StockByVIDItem](data)
}

// FreightCalculate calls /logistic/freightCalculate.
func (c *Client) FreightCalculate(ctx context.Context, req FreightCalculateRequest) ([]FreightOption, error) {
	if strings.TrimSpace(req.StartCountryCode) == "" {
		return nil, fmt.Errorf("startCountryCode is required")
	}
	if strings.TrimSpace(req.EndCountryCode) == "" {
		return nil, fmt.Errorf("endCountryCode is required")
	}
	if len(req.Products) == 0 {
		return nil, fmt.Errorf("products are required")
	}
	data, err := c.doRequestData(ctx, http.MethodPost, "/logistic/freightCalculate", nil, req, true, false)
	if err != nil {
		return nil, err
	}
	return decodeList[FreightOption](data)
}

// GetTrackInfo calls /logistic/trackInfo.
func (c *Client) GetTrackInfo(ctx context.Context, trackNumber string) (*TrackInfo, error) {
	trackNumber = strings.TrimSpace(trackNumber)
	if trackNumber == "" {
		return nil, fmt.Errorf("track number is required")
	}
	q := url.Values{}
	q.Set("trackNumber", trackNumber)
	data, err := c.doRequestData(ctx, http.MethodGet, "/logistic/trackInfo", q, nil, true, false)
	if err != nil {
		return nil, err
	}
	return unmarshalData[TrackInfo](data)
}

// CreateOrderV3 calls /shopping/order/createOrderV3.
func (c *Client) CreateOrderV3(ctx context.Context, req CreateOrderV3Request) (*CreateOrderV3Response, error) {
	if strings.TrimSpace(req.OrderNumber) == "" {
		return nil, fmt.Errorf("orderNumber is required")
	}
	if strings.TrimSpace(req.ShippingCountryCode) == "" {
		return nil, fmt.Errorf("shippingCountryCode is required")
	}
	if strings.TrimSpace(req.ShippingCountry) == "" {
		return nil, fmt.Errorf("shippingCountry is required")
	}
	if strings.TrimSpace(req.ShippingProvince) == "" {
		return nil, fmt.Errorf("shippingProvince is required")
	}
	if strings.TrimSpace(req.ShippingCity) == "" {
		return nil, fmt.Errorf("shippingCity is required")
	}
	if strings.TrimSpace(req.ShippingCustomerName) == "" {
		return nil, fmt.Errorf("shippingCustomerName is required")
	}
	if strings.TrimSpace(req.ShippingAddress) == "" {
		return nil, fmt.Errorf("shippingAddress is required")
	}
	if strings.TrimSpace(req.LogisticName) == "" {
		return nil, fmt.Errorf("logisticName is required")
	}
	if strings.TrimSpace(req.FromCountryCode) == "" {
		return nil, fmt.Errorf("fromCountryCode is required")
	}
	if len(req.Products) == 0 {
		return nil, fmt.Errorf("products are required")
	}

	data, err := c.doRequestData(ctx, http.MethodPost, "/shopping/order/createOrderV3", nil, req, true, true)
	if err != nil {
		return nil, err
	}
	return unmarshalData[CreateOrderV3Response](data)
}

// GetOrderDetail calls /shopping/order/getOrderDetail.
func (c *Client) GetOrderDetail(ctx context.Context, orderID string, features []string) (*OrderDetail, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, fmt.Errorf("orderId is required")
	}
	q := url.Values{}
	q.Set("orderId", orderID)
	for _, feature := range features {
		if f := strings.TrimSpace(feature); f != "" {
			q.Add("features", f)
		}
	}
	data, err := c.doRequestData(ctx, http.MethodGet, "/shopping/order/getOrderDetail", q, nil, true, false)
	if err != nil {
		return nil, err
	}
	return unmarshalData[OrderDetail](data)
}

// ListOrders calls /shopping/order/list.
func (c *Client) ListOrders(ctx context.Context, params ListOrdersParams) (*ListOrdersResponse, error) {
	q := url.Values{}
	if params.PageNum > 0 {
		q.Set("pageNum", strconv.Itoa(params.PageNum))
	}
	if params.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(params.PageSize))
	}
	for _, id := range params.OrderIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			q.Add("orderIds", trimmed)
		}
	}
	if v := strings.TrimSpace(params.ShipmentOrderID); v != "" {
		q.Set("shipmentOrderId", v)
	}
	if v := strings.TrimSpace(params.Status); v != "" {
		q.Set("status", v)
	}

	data, err := c.doRequestData(ctx, http.MethodGet, "/shopping/order/list", q, nil, true, false)
	if err != nil {
		return nil, err
	}
	return unmarshalData[ListOrdersResponse](data)
}
