package gowild_polymarket

import (
	"crypto/ecdsa"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// OrderSide represents the side of an order.
const (
	Buy  = "BUY"
	Sell = "SELL"
)

// OrderType constants for order time-in-force.
const (
	GTC = "GTC" // Good Till Cancel
	FOK = "FOK" // Fill or Kill
	GTD = "GTD" // Good Till Date
)

// buildOrder creates a signed order from human-readable parameters.
// price is in the range (0, 1) — e.g. 0.55 for 55 cents.
// size is the number of outcome tokens (shares) — e.g. 100 for 100 shares.
// side is "BUY" or "SELL".
// funder is the address that holds funds (proxy wallet or EOA).
// sigType is the signature type (0=EOA, 1=POLY_PROXY, 2=POLY_GNOSIS_SAFE).
// negRisk indicates if the market uses the NegRisk exchange.
// tickSize is the market's minimum price increment (e.g. 0.01, 0.001, 0.0001).
// It determines order precision using the same rules as Polymarket's official
// clients: price rounds to tick-size precision, size truncates to 2 decimals,
// and the cash leg keeps price_decimals+2 digits.
// Pass 0 to use the default (0.01).
func buildOrder(
	privateKey *ecdsa.PrivateKey,
	tokenID string,
	price float64,
	size float64,
	side string,
	funder string,
	sigType int,
	chainID int,
	negRisk bool,
	tickSize float64,
) (*signedOrder, error) {
	if price <= 0 || price >= 1 {
		return nil, fmt.Errorf("price must be between 0 and 1 (exclusive), got %f", price)
	}
	if size <= 0 {
		return nil, fmt.Errorf("size must be positive, got %f", size)
	}
	if side != Buy && side != Sell {
		return nil, fmt.Errorf("side must be BUY or SELL, got %s", side)
	}

	signer := privateKeyToAddress(privateKey)

	makerAmt, takerAmt, err := rawOrderAmounts(price, size, side, tickSize)
	if err != nil {
		return nil, err
	}

	salt := generateSalt()
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)

	order := &signedOrder{
		Salt:          salt,
		Maker:         funder, // Funder holds funds (proxy wallet or EOA)
		Signer:        signer, // EOA that signs
		TokenID:       tokenID,
		MakerAmount:   strconv.FormatInt(makerAmt, 10),
		TakerAmount:   strconv.FormatInt(takerAmt, 10),
		Expiration:    "0",  // No expiration (GTC)
		Side:          side, // API payload expects "BUY"/"SELL"
		SignatureType: sigType,
		Timestamp:     timestamp,
		Metadata:      zeroBytes32,
		Builder:       zeroBytes32,
	}

	sig, err := signOrder(privateKey, order, chainID, negRisk)
	if err != nil {
		return nil, fmt.Errorf("failed to sign order: %w", err)
	}
	order.Signature = sig

	return order, nil
}

// buildOrderWithExpiration creates a signed order with a specific expiration timestamp.
func buildOrderWithExpiration(
	privateKey *ecdsa.PrivateKey,
	tokenID string,
	price float64,
	size float64,
	side string,
	chainID int,
	negRisk bool,
	expirationUnix int64,
	tickSize float64,
) (*signedOrder, error) {
	order, err := buildOrder(privateKey, tokenID, price, size, side, privateKeyToAddress(privateKey), SigTypeEOA, chainID, negRisk, tickSize)
	if err != nil {
		return nil, err
	}
	order.Expiration = strconv.FormatInt(expirationUnix, 10)

	// Re-sign with the new expiration
	sig, err := signOrder(privateKey, order, chainID, negRisk)
	if err != nil {
		return nil, fmt.Errorf("failed to sign order: %w", err)
	}
	order.Signature = sig

	return order, nil
}

type orderRoundingConfig struct {
	priceDecimals  int
	sizeDecimals   int
	amountDecimals int
}

func roundingConfigForTickSize(tickSize float64) orderRoundingConfig {
	if tickSize <= 0 {
		tickSize = 0.01
	}
	priceDecimals := int(math.Round(-math.Log10(tickSize)))
	if priceDecimals < 1 {
		priceDecimals = 1
	}
	if priceDecimals > 4 {
		priceDecimals = 4
	}
	return orderRoundingConfig{
		priceDecimals:  priceDecimals,
		sizeDecimals:   2,
		amountDecimals: priceDecimals + 2,
	}
}

func rawOrderAmounts(price, size float64, side string, tickSize float64) (int64, int64, error) {
	cfg := roundingConfigForTickSize(tickSize)
	priceScale := pow10Int(cfg.priceDecimals)

	rawPrice := roundNormalToScaledInt(price, cfg.priceDecimals)
	if rawPrice <= 0 || rawPrice >= priceScale {
		roundedPrice := float64(rawPrice) / float64(priceScale)
		return 0, 0, fmt.Errorf("price rounds outside (0, 1) for tick size %.4g: %.6f", tickSize, roundedPrice)
	}
	rawSize := roundDownToScaledInt(size, cfg.sizeDecimals)
	if rawSize <= 0 {
		return 0, 0, fmt.Errorf("size rounds to zero at 2-decimal order precision")
	}

	shareRaw := rawSize * pow10Int(6-cfg.sizeDecimals)
	notionalRaw := rawPrice * rawSize * pow10Int(6-cfg.amountDecimals)

	if side == Buy {
		return notionalRaw, shareRaw, nil
	}
	return shareRaw, notionalRaw, nil
}

// priceToMakerTakerAmounts converts a human-readable price and size to
// makerAmount and takerAmount in raw units (6 on-chain decimals), rounded the
// same way as Polymarket's official clients.
func priceToMakerTakerAmounts(price, size float64, side string, tickSize float64) (makerAmount, takerAmount *big.Int) {
	makerRaw, takerRaw, err := rawOrderAmounts(price, size, side, tickSize)
	if err != nil {
		return big.NewInt(0), big.NewInt(0)
	}
	makerAmount = big.NewInt(makerRaw)
	takerAmount = big.NewInt(takerRaw)
	return
}

func roundNormalToScaledInt(value float64, decimals int) int64 {
	return scaleDecimalString(value, decimals, roundHalfAwayFromZero)
}

func roundDownToScaledInt(value float64, decimals int) int64 {
	return scaleDecimalString(value, decimals, roundTowardNegativeInfinity)
}

func pow10Int(exp int) int64 {
	result := int64(1)
	for i := 0; i < exp; i++ {
		result *= 10
	}
	return result
}

type decimalRoundingMode int

const (
	roundHalfAwayFromZero decimalRoundingMode = iota
	roundTowardNegativeInfinity
)

func scaleDecimalString(value float64, decimals int, mode decimalRoundingMode) int64 {
	if decimals < 0 {
		return 0
	}

	text := strconv.FormatFloat(value, 'f', -1, 64)
	sign := int64(1)
	if strings.HasPrefix(text, "-") {
		sign = -1
		text = text[1:]
	}

	wholeText, fracText, foundDot := strings.Cut(text, ".")
	if !foundDot {
		fracText = ""
	}
	if wholeText == "" {
		wholeText = "0"
	}

	whole, err := strconv.ParseInt(wholeText, 10, 64)
	if err != nil {
		return 0
	}

	scale := pow10Int(decimals)
	scaled := whole * scale

	if len(fracText) < decimals {
		fracText += strings.Repeat("0", decimals-len(fracText))
	}

	keepText := fracText
	if len(keepText) > decimals {
		keepText = keepText[:decimals]
	}
	if keepText != "" {
		frac, err := strconv.ParseInt(keepText, 10, 64)
		if err != nil {
			return 0
		}
		scaled += frac
	}

	droppedText := ""
	if len(fracText) > decimals {
		droppedText = fracText[decimals:]
	}

	switch mode {
	case roundHalfAwayFromZero:
		if shouldRoundAwayFromZero(droppedText) {
			scaled++
		}
	case roundTowardNegativeInfinity:
		if sign < 0 && hasNonZeroDigit(droppedText) {
			scaled++
		}
	}

	return sign * scaled
}

func shouldRoundAwayFromZero(droppedText string) bool {
	if droppedText == "" {
		return false
	}
	return droppedText[0] >= '5'
}

func hasNonZeroDigit(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] != '0' {
			return true
		}
	}
	return false
}
