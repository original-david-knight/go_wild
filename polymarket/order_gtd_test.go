package gowild_polymarket

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func gtdTestClient(t *testing.T) *Client {
	t.Helper()
	key := testKey(t)
	addr := privateKeyToAddress(key)
	return &Client{
		privateKey:    key,
		address:       addr,
		funder:        addr,
		signatureType: SigTypeEOA,
		chainID:       137,
	}
}

const gtdTestTokenID = "52114319501245915516055106046884209969926127482827954674443846427166160274944"

func TestBuildGTDPlaceRequest_CustomExpiration(t *testing.T) {
	c := gtdTestClient(t)
	exp := time.Now().Add(72 * time.Hour).Unix()

	req, err := c.buildGTDPlaceRequest(gtdTestTokenID, 0.91, 10, Buy, false, 0.01, exp)
	if err != nil {
		t.Fatalf("buildGTDPlaceRequest: %v", err)
	}
	if req.OrderType != GTD {
		t.Errorf("order_type = %q, want %q", req.OrderType, GTD)
	}
	if got, want := req.Order.Expiration, strconv.FormatInt(exp, 10); got != want {
		t.Errorf("expiration = %q, want %q (exact custom timestamp, not a now-relative TTL)", got, want)
	}
	if req.Order.Expiration == "0" {
		t.Errorf("expiration must not be 0 for a GTD order")
	}
}

func TestBuildGTDOrder_SignatureExcludesExpiration(t *testing.T) {
	c := gtdTestClient(t)
	exp1 := time.Now().Add(72 * time.Hour).Unix()
	exp2 := exp1 + 3600

	order, err := c.buildSignedGTDOrder(gtdTestTokenID, 0.91, 10, Buy, false, 0.01, exp1)
	if err != nil {
		t.Fatalf("buildSignedGTDOrder: %v", err)
	}
	// The order payload still carries the exact GTD expiration for the CLOB to
	// enforce off-chain.
	if got, want := order.Expiration, strconv.FormatInt(exp1, 10); got != want {
		t.Fatalf("expiration = %q, want %q in the order payload", got, want)
	}

	// Re-sign the SAME order (identical salt) with a different expiration. The
	// on-chain V2 Order struct has no expiration field, so the signature must NOT
	// change — signing expiration would make the CLOB reject the order with
	// "invalid EOA signature".
	other := *order
	other.Expiration = strconv.FormatInt(exp2, 10)
	sig2, err := signOrder(c.privateKey, &other, c.chainID, false)
	if err != nil {
		t.Fatalf("signOrder: %v", err)
	}
	if sig2 != order.Signature {
		t.Fatalf("signature changed with expiration; expiration must be excluded from the V2 signed hash")
	}
}

func TestBuildGTDOrder_RejectsNonFutureExpiration(t *testing.T) {
	c := gtdTestClient(t)
	now := time.Now().Unix()

	for _, exp := range []int64{0, -1, now - 1, now} {
		req, err := c.buildGTDPlaceRequest(gtdTestTokenID, 0.91, 10, Buy, false, 0.01, exp)
		var target *ExpirationNotFutureError
		if !errors.As(err, &target) {
			t.Fatalf("expiration %d: want ExpirationNotFutureError, got %v", exp, err)
		}
		if req != nil {
			t.Fatalf("expiration %d: expected no signed order, got %+v", exp, req)
		}
	}
}
