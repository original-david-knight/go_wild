package gowild_polymarket

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"
)

func TestPriceToMakerTakerAmounts_Buy(t *testing.T) {
	// BUY 100 shares at $0.55
	maker, taker := priceToMakerTakerAmounts(0.55, 100, Buy, 0)

	// makerAmount = 0.55 * 100 * 1e6 = 55_000_000 (USDC the buyer pays)
	expectedMaker := big.NewInt(55_000_000)
	if maker.Cmp(expectedMaker) != 0 {
		t.Errorf("BUY makerAmount: got %s, want %s", maker, expectedMaker)
	}

	// takerAmount = 100 * 1e6 = 100_000_000 (tokens the buyer receives)
	expectedTaker := big.NewInt(100_000_000)
	if taker.Cmp(expectedTaker) != 0 {
		t.Errorf("BUY takerAmount: got %s, want %s", taker, expectedTaker)
	}
}

func TestPriceToMakerTakerAmounts_Sell(t *testing.T) {
	// SELL 50 shares at $0.72
	maker, taker := priceToMakerTakerAmounts(0.72, 50, Sell, 0)

	// makerAmount = 50 * 1e6 = 50_000_000 (tokens the seller delivers)
	expectedMaker := big.NewInt(50_000_000)
	if maker.Cmp(expectedMaker) != 0 {
		t.Errorf("SELL makerAmount: got %s, want %s", maker, expectedMaker)
	}

	// takerAmount = 0.72 * 50 * 1e6 = 36_000_000 (USDC the seller receives)
	expectedTaker := big.NewInt(36_000_000)
	if taker.Cmp(expectedTaker) != 0 {
		t.Errorf("SELL takerAmount: got %s, want %s", taker, expectedTaker)
	}
}

func TestBuildOrder_InvalidPrice(t *testing.T) {
	key := testKey(t)

	_, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0, 100, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err == nil {
		t.Error("expected error for price=0")
	}

	_, err = buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 1.0, 100, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err == nil {
		t.Error("expected error for price=1")
	}

	_, err = buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", -0.5, 100, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err == nil {
		t.Error("expected error for negative price")
	}
}

func TestBuildOrder_InvalidSize(t *testing.T) {
	key := testKey(t)

	_, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.5, 0, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err == nil {
		t.Error("expected error for size=0")
	}

	_, err = buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.5, -10, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err == nil {
		t.Error("expected error for negative size")
	}
}

func TestBuildOrder_InvalidSide(t *testing.T) {
	key := testKey(t)

	_, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.5, 100, "INVALID", privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err == nil {
		t.Error("expected error for invalid side")
	}
}

func TestBuildOrder_Success(t *testing.T) {
	key := testKey(t)
	addr := privateKeyToAddress(key)

	order, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.55, 100, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err != nil {
		t.Fatalf("buildOrder failed: %v", err)
	}

	if order.Maker != addr {
		t.Errorf("expected maker %s, got %s", addr, order.Maker)
	}
	if order.Signer != addr {
		t.Errorf("expected signer %s, got %s", addr, order.Signer)
	}
	if order.TokenID != "52114319501245915516055106046884209969926127482827954674443846427166160274944" {
		t.Errorf("expected tokenID token123, got %s", order.TokenID)
	}
	if order.Side != Buy {
		t.Errorf("expected side %s, got %s", Buy, order.Side)
	}
	if order.MakerAmount != "55000000" {
		t.Errorf("expected makerAmount 55000000, got %s", order.MakerAmount)
	}
	if order.TakerAmount != "100000000" {
		t.Errorf("expected takerAmount 100000000, got %s", order.TakerAmount)
	}
	if order.Expiration != "0" {
		t.Errorf("expected expiration 0, got %s", order.Expiration)
	}
	if order.Signature == "" {
		t.Error("expected non-empty signature")
	}
	if order.Signature[:2] != "0x" {
		t.Errorf("expected 0x signature prefix, got %s", order.Signature[:2])
	}
	if order.Salt == nil || order.Salt.Cmp(big.NewInt(0)) <= 0 {
		t.Error("expected positive salt")
	}
}

func TestBuildOrder_SerializesBuySellSidesForAPI(t *testing.T) {
	key := testKey(t)

	buyOrder, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.55, 1, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err != nil {
		t.Fatalf("buildOrder BUY failed: %v", err)
	}
	buyJSON, err := json.Marshal(buyOrder)
	if err != nil {
		t.Fatalf("marshal BUY order failed: %v", err)
	}
	if !strings.Contains(string(buyJSON), `"side":"BUY"`) {
		t.Fatalf("expected BUY payload side to be BUY, got %s", string(buyJSON))
	}

	sellOrder, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.55, 1, Sell, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err != nil {
		t.Fatalf("buildOrder SELL failed: %v", err)
	}
	sellJSON, err := json.Marshal(sellOrder)
	if err != nil {
		t.Fatalf("marshal SELL order failed: %v", err)
	}
	if !strings.Contains(string(sellJSON), `"side":"SELL"`) {
		t.Fatalf("expected SELL payload side to be SELL, got %s", string(sellJSON))
	}
}

func TestBuildOrder_SerializesCLOBV2Fields(t *testing.T) {
	key := testKey(t)

	order, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.55, 1, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err != nil {
		t.Fatalf("buildOrder failed: %v", err)
	}

	if order.Timestamp == "" {
		t.Fatal("expected timestamp to be set")
	}
	if order.Metadata != zeroBytes32 {
		t.Fatalf("expected zero metadata, got %s", order.Metadata)
	}
	if order.Builder != zeroBytes32 {
		t.Fatalf("expected zero builder, got %s", order.Builder)
	}

	orderJSON, err := json.Marshal(order)
	if err != nil {
		t.Fatalf("marshal order failed: %v", err)
	}
	body := string(orderJSON)
	for _, want := range []string{`"timestamp":"`, `"metadata":"` + zeroBytes32 + `"`, `"builder":"` + zeroBytes32 + `"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected V2 field %s in order payload, got %s", want, body)
		}
	}
	for _, legacy := range []string{`"taker"`, `"nonce"`, `"feeRateBps"`} {
		if strings.Contains(body, legacy) {
			t.Fatalf("did not expect legacy V1 field %s in V2 order payload: %s", legacy, body)
		}
	}
}

func TestBuildOrder_NegRisk(t *testing.T) {
	key := testKey(t)

	orderNormal, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.5, 100, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0)
	if err != nil {
		t.Fatalf("buildOrder (normal) failed: %v", err)
	}

	orderNegRisk, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.5, 100, Buy, privateKeyToAddress(key), SigTypeEOA, 137, true, 0)
	if err != nil {
		t.Fatalf("buildOrder (negRisk) failed: %v", err)
	}

	// Signatures should differ because the verifying contract address differs
	if orderNormal.Signature == orderNegRisk.Signature {
		t.Error("expected different signatures for normal vs negRisk orders")
	}
}

func TestRoundingConfigForTickSize(t *testing.T) {
	tests := []struct {
		tick          float64
		priceDecimals int
		sizeDecimals  int
		amountDigits  int
	}{
		{0.1, 1, 2, 3},
		{0.01, 2, 2, 4},
		{0.001, 3, 2, 5},
		{0.0001, 4, 2, 6},
		{0, 2, 2, 4},
		{-1, 2, 2, 4},
	}
	for _, tc := range tests {
		result := roundingConfigForTickSize(tc.tick)
		if result.priceDecimals != tc.priceDecimals || result.sizeDecimals != tc.sizeDecimals || result.amountDecimals != tc.amountDigits {
			t.Errorf("roundingConfigForTickSize(%g) = %+v, want price=%d size=%d amount=%d", tc.tick, result, tc.priceDecimals, tc.sizeDecimals, tc.amountDigits)
		}
	}
}

func TestRoundDownToScaledInt(t *testing.T) {
	tests := []struct {
		value    float64
		decimals int
		expected int64
	}{
		{29.825, 2, 2982},
		{29.8299999, 2, 2982},
		{1360.33, 2, 136033},
		{0.009, 2, 0},
	}
	for _, tc := range tests {
		result := roundDownToScaledInt(tc.value, tc.decimals)
		if result != tc.expected {
			t.Errorf("roundDownToScaledInt(%f, %d) = %d, want %d", tc.value, tc.decimals, result, tc.expected)
		}
	}
}

func TestBuildOrder_UsesOfficialClobRounding(t *testing.T) {
	key := testKey(t)
	tokenID := "52114319501245915516055106046884209969926127482827954674443846427166160274944"

	// Official clients always truncate size to 2 decimals, even when the market
	// tick size supports more price precision.
	order, err := buildOrder(key, tokenID, 0.36, 29.825, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0.01)
	if err != nil {
		t.Fatalf("buildOrder failed: %v", err)
	}
	if order.TakerAmount != "29820000" {
		t.Errorf("expected takerAmount 29820000 (29.82 shares), got %s", order.TakerAmount)
	}
	if order.MakerAmount != "10735200" {
		t.Errorf("expected makerAmount 10735200 (0.36 * 29.82), got %s", order.MakerAmount)
	}

	// The cash leg keeps price_decimals+2 digits, matching the reference clients.
	order3, err := buildOrder(key, tokenID, 0.35, 29.825, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0.01)
	if err != nil {
		t.Fatalf("buildOrder failed: %v", err)
	}
	if order3.TakerAmount != "29820000" {
		t.Errorf("expected takerAmount 29820000, got %s", order3.TakerAmount)
	}
	if order3.MakerAmount != "10437000" {
		t.Errorf("expected makerAmount 10437000 (0.35 * 29.82 * 1e6), got %s", order3.MakerAmount)
	}

	// Regression for the screenshot path: SELL 1360.33 shares at 0.024 on a
	// 0.001-tick market must preserve 5 cash decimals, not truncate to 3/4.
	order2, err := buildOrder(key, tokenID, 0.024, 1360.339, Sell, privateKeyToAddress(key), SigTypeEOA, 137, false, 0.001)
	if err != nil {
		t.Fatalf("buildOrder failed: %v", err)
	}
	if order2.MakerAmount != "1360330000" {
		t.Errorf("expected makerAmount 1360330000 (1360.33 shares), got %s", order2.MakerAmount)
	}
	if order2.TakerAmount != "32647920" {
		t.Errorf("expected takerAmount 32647920 ($32.64792), got %s", order2.TakerAmount)
	}
}

func TestBuildOrder_RoundsHalfTickPricesUsingDecimalInput(t *testing.T) {
	key := testKey(t)
	tokenID := "52114319501245915516055106046884209969926127482827954674443846427166160274944"

	order, err := buildOrder(key, tokenID, 0.575, 1, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0.01)
	if err != nil {
		t.Fatalf("buildOrder failed: %v", err)
	}
	if order.MakerAmount != "580000" {
		t.Fatalf("expected makerAmount 580000 (0.58 * 1 share), got %s", order.MakerAmount)
	}
	if order.TakerAmount != "1000000" {
		t.Fatalf("expected takerAmount 1000000 (1 share), got %s", order.TakerAmount)
	}
}
