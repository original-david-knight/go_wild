package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/mr-tron/base58"
	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

const (
	polygonUSDCAddress = "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359"
	solanaUSDCMint     = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	usdcDecimals       = 6
)

// PaywallHandlers holds the database reference for paywall endpoints.
type PaywallHandlers struct {
	db         gowild_data.Database
	storageDir string
}

// NewPaywallHandlers creates paywall handlers.
func NewPaywallHandlers(db gowild_data.Database) *PaywallHandlers {
	dir := strings.TrimSpace(os.Getenv("PAYWALL_STORAGE_DIR"))
	if dir == "" {
		dir = "/var/data/paywall"
	}
	return &PaywallHandlers{db: db, storageDir: dir}
}

// handleCreate handles POST /api/v1/paywall/create (premium-authenticated, multipart).
func (ph *PaywallHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())
	if agentID == "" {
		writeBadRequest(w, "missing agent ID")
		return
	}

	// Parse multipart (50MB limit)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeBadRequest(w, "failed to parse multipart form: "+err.Error())
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	priceUSDC := strings.TrimSpace(r.FormValue("price_usdc"))
	chain := strings.TrimSpace(r.FormValue("chain"))
	walletAddress := strings.TrimSpace(r.FormValue("wallet_address"))

	// Validate
	if title == "" {
		writeBadRequest(w, "title is required")
		return
	}
	if priceUSDC == "" {
		writeBadRequest(w, "price_usdc is required")
		return
	}
	price, err := strconv.ParseFloat(priceUSDC, 64)
	if err != nil || price <= 0 {
		writeBadRequest(w, "price_usdc must be a positive number")
		return
	}
	if chain != "polygon" && chain != "solana" {
		writeBadRequest(w, "chain must be 'polygon' or 'solana'")
		return
	}
	if walletAddress == "" {
		writeBadRequest(w, "wallet_address is required")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeBadRequest(w, "file is required")
		return
	}
	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		writeInternalError(w, "failed to read uploaded file")
		return
	}

	// Generate product ID
	idBytes := make([]byte, 4)
	if _, err := rand.Read(idBytes); err != nil {
		writeInternalError(w, "failed to generate product ID")
		return
	}
	productID := "prod_" + hex.EncodeToString(idBytes)
	fileName := filepath.Base(header.Filename)

	// Create storage directory and write file
	storageDir := filepath.Join(ph.storageDir, productID)
	if err := os.MkdirAll(storageDir, 0700); err != nil {
		writeInternalError(w, "failed to create storage directory")
		return
	}

	storagePath := filepath.Join(storageDir, fileName)
	if err := os.WriteFile(storagePath, fileData, 0600); err != nil {
		writeInternalError(w, "failed to store file")
		return
	}

	// Insert product record
	product := &data.PaywallProduct{
		ID:            productID,
		AgentID:       agentID,
		Title:         title,
		Description:   description,
		PriceUSDC:     priceUSDC,
		Chain:         chain,
		WalletAddress: walletAddress,
		StoragePath:   storagePath,
		FileName:      fileName,
		FileSize:      int64(len(fileData)),
		Status:        "active",
	}

	if err := data.CreatePaywallProductUnscoped(r.Context(), ph.db, product); err != nil {
		os.RemoveAll(storageDir)
		writeInternalError(w, "failed to create product: "+err.Error())
		return
	}

	// Build full checkout URL from request host.
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	host := r.Host
	checkoutURL := scheme + "://" + host + "/paywall/" + productID

	writeJSON(w, http.StatusOK, map[string]any{
		"product_id":   productID,
		"checkout_url": checkoutURL,
		"file_name":    fileName,
		"file_size":    len(fileData),
	})
}

// HandlePaywallRoute dispatches /paywall/{id}, /paywall/{id}/info, /paywall/{id}/verify.
func (ph *PaywallHandlers) HandlePaywallRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/paywall/")
	if path == "" {
		writeBadRequest(w, "missing product ID")
		return
	}

	switch {
	case strings.HasSuffix(path, "/info"):
		ph.handleProductInfo(w, r)
	case strings.HasSuffix(path, "/verify"):
		ph.handleVerify(w, r)
	default:
		ph.handleCheckout(w, r)
	}
}

// HandlePaywallDownload handles GET /paywall/dl/{token}.
func (ph *PaywallHandlers) HandlePaywallDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "method not allowed")
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/paywall/dl/")
	if token == "" || token == r.URL.Path {
		writeBadRequest(w, "missing download token")
		return
	}

	purchase, err := data.GetPaywallPurchaseByToken(r.Context(), ph.db, token)
	if err != nil || purchase == nil {
		writeNotFound(w, "invalid or expired download token")
		return
	}

	if time.Now().After(purchase.TokenExpiresAt) {
		writeError(w, http.StatusGone, ErrorResponse{Error: "EXPIRED", Message: "download token has expired"})
		return
	}

	product, err := data.GetPaywallProductUnscoped(r.Context(), ph.db, purchase.ProductID)
	if err != nil {
		writeInternalError(w, "product not found")
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, product.FileName))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, product.StoragePath)
}

// handleProductInfo handles GET /paywall/{product_id}/info.
func (ph *PaywallHandlers) handleProductInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "method not allowed")
		return
	}

	productID := extractPaywallProductID(r.URL.Path, "/info")
	if productID == "" {
		writeBadRequest(w, "missing product ID")
		return
	}

	product, err := data.GetPaywallProductUnscoped(r.Context(), ph.db, productID)
	if err != nil {
		writeNotFound(w, "product not found")
		return
	}
	if product.Status != "active" {
		writeError(w, http.StatusGone, ErrorResponse{Error: "GONE", Message: "product is no longer available"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":             product.ID,
		"title":          product.Title,
		"description":    product.Description,
		"price_usdc":     product.PriceUSDC,
		"chain":          product.Chain,
		"wallet_address": product.WalletAddress,
		"file_name":      product.FileName,
		"file_size":      product.FileSize,
	})
}

// handleVerify handles POST /paywall/{product_id}/verify.
func (ph *PaywallHandlers) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "method not allowed")
		return
	}

	productID := extractPaywallProductID(r.URL.Path, "/verify")
	if productID == "" {
		writeBadRequest(w, "missing product ID")
		return
	}

	var input struct {
		TxHash         string `json:"tx_hash"`
		BuyerAddress   string `json:"buyer_address"`
		BuyerSignature string `json:"buyer_signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeBadRequest(w, "invalid JSON: "+err.Error())
		return
	}
	if input.TxHash == "" {
		writeBadRequest(w, "tx_hash is required")
		return
	}
	if input.BuyerAddress == "" {
		writeBadRequest(w, "buyer_address is required")
		return
	}
	if strings.TrimSpace(input.BuyerSignature) == "" {
		writeBadRequest(w, "buyer_signature is required")
		return
	}

	product, err := data.GetPaywallProductUnscoped(r.Context(), ph.db, productID)
	if err != nil {
		writeNotFound(w, "product not found")
		return
	}
	if product.Status != "active" {
		writeError(w, http.StatusGone, ErrorResponse{Error: "GONE", Message: "product is no longer available"})
		return
	}

	if err := verifyBuyerProof(product.Chain, productID, input.TxHash, input.BuyerAddress, input.BuyerSignature); err != nil {
		writeBadRequest(w, "buyer proof verification failed: "+err.Error())
		return
	}

	// Replay prevention
	existing, err := data.GetPaywallPurchaseByTxHash(r.Context(), ph.db, input.TxHash)
	if err != nil {
		writeInternalError(w, "failed to check transaction")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, ErrorResponse{Error: "REPLAY", Message: "transaction already used for a purchase"})
		return
	}

	// Verify on-chain
	verification, err := verifyOnChainPayment(r.Context(), product, input.TxHash)
	if err != nil {
		writeBadRequest(w, "verification failed: "+err.Error())
		return
	}

	// Bind purchase to buyer wallet
	if !buyerAddressMatches(product.Chain, verification.payerAddress, input.BuyerAddress) {
		writeBadRequest(w, "buyer_address does not match the transaction payer")
		return
	}

	// Create purchase record
	purchaseID := make([]byte, 4)
	rand.Read(purchaseID)
	bt := verification.blockTime
	purchase := &data.PaywallPurchase{
		ID:           "pur_" + hex.EncodeToString(purchaseID),
		ProductID:    productID,
		TxHash:       input.TxHash,
		Chain:        product.Chain,
		PayerAddress: verification.payerAddress,
		AmountUSDC:   verification.amountUSDC,
		BlockTime:    &bt,
	}

	if err := data.CreatePaywallPurchase(r.Context(), ph.db, purchase); err != nil {
		if data.IsUniqueConstraintError(err) {
			writeError(w, http.StatusConflict, ErrorResponse{Error: "REPLAY", Message: "transaction already used for a purchase"})
			return
		}
		writeInternalError(w, "failed to record purchase: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "verified",
		"download_token": purchase.DownloadToken,
		"expires_at":     purchase.TokenExpiresAt.Format(time.RFC3339),
	})
}

// handleCheckout serves the checkout HTML page.
func (ph *PaywallHandlers) handleCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "method not allowed")
		return
	}

	productID := strings.TrimPrefix(r.URL.Path, "/paywall/")
	if productID == "" || strings.Contains(productID, "/") {
		writeBadRequest(w, "invalid product ID")
		return
	}

	product, err := data.GetPaywallProductUnscoped(r.Context(), ph.db, productID)
	if err != nil || product == nil {
		writeNotFound(w, "product not found")
		return
	}
	if product.Status != "active" {
		writeError(w, http.StatusGone, ErrorResponse{Error: "GONE", Message: "product is no longer available"})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, checkoutHTML)
}

// --- On-chain verification ---

type verificationResult struct {
	payerAddress string
	amountUSDC   string
	blockTime    time.Time
}

func verifyOnChainPayment(ctx context.Context, product *data.PaywallProduct, txHash string) (*verificationResult, error) {
	switch product.Chain {
	case "polygon":
		return verifyPolygonPayment(ctx, product, txHash)
	case "solana":
		return verifySolanaPayment(ctx, product, txHash)
	default:
		return nil, fmt.Errorf("unsupported chain: %s", product.Chain)
	}
}

func verifyPolygonPayment(ctx context.Context, product *data.PaywallProduct, txHash string) (*verificationResult, error) {
	rpcURL := resolvePolygonRPC()

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Polygon RPC: %w", err)
	}
	defer client.Close()

	hash := common.HexToHash(txHash)
	receipt, err := client.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction receipt: %w", err)
	}
	if receipt.Status != 1 {
		return nil, fmt.Errorf("transaction failed (status=%d)", receipt.Status)
	}

	transferSig := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	usdcAddr := common.HexToAddress(polygonUSDCAddress)
	walletAddr := common.HexToAddress(product.WalletAddress)
	requiredAmount, err := parseUSDCAmount(product.PriceUSDC)
	if err != nil {
		return nil, fmt.Errorf("invalid product price: %w", err)
	}

	for _, vLog := range receipt.Logs {
		if vLog.Address != usdcAddr {
			continue
		}
		if len(vLog.Topics) != 3 || vLog.Topics[0] != transferSig {
			continue
		}
		to := common.BytesToAddress(vLog.Topics[2].Bytes())
		if !strings.EqualFold(to.Hex(), walletAddr.Hex()) {
			continue
		}
		amount := new(big.Int).SetBytes(vLog.Data)
		if amount.Cmp(requiredAmount) < 0 {
			continue
		}

		// Block-time validation: reject transactions from before the product was created
		header, err := client.HeaderByNumber(ctx, receipt.BlockNumber)
		if err != nil {
			return nil, fmt.Errorf("failed to get block header: %w", err)
		}
		blockTime := time.Unix(int64(header.Time), 0)
		if err := validatePaywallBlockTime(blockTime, product.CreatedAt); err != nil {
			return nil, err
		}

		from := common.BytesToAddress(vLog.Topics[1].Bytes())
		return &verificationResult{
			payerAddress: from.Hex(),
			amountUSDC:   formatUSDCAmount(amount),
			blockTime:    blockTime,
		}, nil
	}

	return nil, fmt.Errorf("no matching USDC transfer to %s found in transaction logs", product.WalletAddress)
}

func verifySolanaPayment(ctx context.Context, product *data.PaywallProduct, txSignature string) (*verificationResult, error) {
	rpcURL := resolveSolanaRPC()
	requiredAmount, err := parseUSDCAmount(product.PriceUSDC)
	if err != nil {
		return nil, fmt.Errorf("invalid product price: %w", err)
	}

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "getTransaction",
		"params": []any{
			txSignature,
			map[string]any{
				"encoding":                       "jsonParsed",
				"commitment":                     "confirmed",
				"maxSupportedTransactionVersion": 0,
			},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", rpcURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create RPC request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Solana RPC request failed: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result *solanaTransaction `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("failed to parse Solana RPC response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("Solana RPC error: %s", rpcResp.Error.Message)
	}
	if rpcResp.Result == nil {
		return nil, fmt.Errorf("transaction not found (may not be confirmed yet)")
	}

	tx := rpcResp.Result
	if tx.Meta.Err != nil {
		return nil, fmt.Errorf("transaction failed")
	}

	// Block-time validation: reject transactions from before the product was created
	if tx.BlockTime == nil {
		return nil, fmt.Errorf("transaction has no block time")
	}
	blockTime := time.Unix(*tx.BlockTime, 0)
	if err := validatePaywallBlockTime(blockTime, product.CreatedAt); err != nil {
		return nil, err
	}

	walletAddr := product.WalletAddress
	for _, post := range tx.Meta.PostTokenBalances {
		if post.Mint != solanaUSDCMint || strings.TrimSpace(post.Owner) != strings.TrimSpace(walletAddr) {
			continue
		}
		var preAmount *big.Int
		for _, pre := range tx.Meta.PreTokenBalances {
			if pre.AccountIndex == post.AccountIndex && pre.Mint == solanaUSDCMint {
				preAmount, _ = new(big.Int).SetString(pre.UITokenAmount.Amount, 10)
				break
			}
		}
		if preAmount == nil {
			preAmount = big.NewInt(0)
		}
		postAmount, _ := new(big.Int).SetString(post.UITokenAmount.Amount, 10)
		if postAmount == nil {
			continue
		}
		delta := new(big.Int).Sub(postAmount, preAmount)
		if delta.Cmp(requiredAmount) >= 0 {
			payer := "unknown"
			for _, preBal := range tx.Meta.PreTokenBalances {
				if preBal.Mint != solanaUSDCMint || preBal.AccountIndex == post.AccountIndex {
					continue
				}
				for j := range tx.Meta.PostTokenBalances {
					pb := &tx.Meta.PostTokenBalances[j]
					if pb.AccountIndex == preBal.AccountIndex {
						preAmt, _ := new(big.Int).SetString(preBal.UITokenAmount.Amount, 10)
						postAmt, _ := new(big.Int).SetString(pb.UITokenAmount.Amount, 10)
						if preAmt != nil && postAmt != nil && preAmt.Cmp(postAmt) > 0 {
							payer = preBal.Owner
						}
						break
					}
				}
				if payer != "unknown" {
					break
				}
			}
			return &verificationResult{
				payerAddress: payer,
				amountUSDC:   formatUSDCAmount(delta),
				blockTime:    blockTime,
			}, nil
		}
	}

	return nil, fmt.Errorf("no matching USDC transfer to %s found in transaction", walletAddr)
}

// --- Solana types ---

type solanaTransaction struct {
	BlockTime *int64                `json:"blockTime"`
	Meta      solanaTransactionMeta `json:"meta"`
}

type solanaTransactionMeta struct {
	Err               any                  `json:"err"`
	PreTokenBalances  []solanaTokenBalance `json:"preTokenBalances"`
	PostTokenBalances []solanaTokenBalance `json:"postTokenBalances"`
}

type solanaTokenBalance struct {
	AccountIndex  int                 `json:"accountIndex"`
	Mint          string              `json:"mint"`
	Owner         string              `json:"owner"`
	UITokenAmount solanaUITokenAmount `json:"uiTokenAmount"`
}

type solanaUITokenAmount struct {
	Amount   string  `json:"amount"`
	Decimals int     `json:"decimals"`
	UIAmount float64 `json:"uiAmount"`
}

// --- Helpers ---

func extractPaywallProductID(path, suffix string) string {
	path = strings.TrimPrefix(path, "/paywall/")
	path = strings.TrimSuffix(path, suffix)
	if path == "" || strings.Contains(path, "/") {
		return ""
	}
	return path
}

func resolvePolygonRPC() string {
	if url := strings.TrimSpace(os.Getenv("WALLET_POLYGON_RPC_URL")); url != "" {
		return url
	}
	if url := strings.TrimSpace(os.Getenv("POLYMARKET_RPC_URL")); url != "" {
		return url
	}
	return "https://polygon-bor-rpc.publicnode.com"
}

func resolveSolanaRPC() string {
	if url := strings.TrimSpace(os.Getenv("SOLANA_RPC_URL")); url != "" {
		return url
	}
	return "https://api.mainnet-beta.solana.com"
}

func parseUSDCAmount(priceStr string) (*big.Int, error) {
	parts := strings.Split(priceStr, ".")
	if len(parts) > 2 {
		return nil, fmt.Errorf("invalid price format: %s", priceStr)
	}
	intPart := parts[0]
	fracPart := ""
	if len(parts) == 2 {
		fracPart = parts[1]
	}
	for len(fracPart) < usdcDecimals {
		fracPart += "0"
	}
	fracPart = fracPart[:usdcDecimals]
	amount, ok := new(big.Int).SetString(intPart+fracPart, 10)
	if !ok {
		return nil, fmt.Errorf("invalid price: %s", priceStr)
	}
	return amount, nil
}

func formatUSDCAmount(amount *big.Int) string {
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(usdcDecimals)), nil)
	whole := new(big.Int).Div(amount, divisor)
	frac := new(big.Int).Mod(amount, divisor)
	return fmt.Sprintf("%s.%06s", whole.String(), frac.String())
}

func buildBuyerProofMessage(productID, txHash string) string {
	return fmt.Sprintf("gowild-paywall-verify-v1\nproduct_id:%s\ntx_hash:%s",
		strings.TrimSpace(productID), strings.TrimSpace(txHash))
}

func verifyBuyerProof(chain, productID, txHash, buyerAddress, buyerSignature string) error {
	message := buildBuyerProofMessage(productID, txHash)

	switch strings.TrimSpace(chain) {
	case "polygon":
		return verifyPolygonBuyerProof(message, buyerAddress, buyerSignature)
	case "solana":
		return verifySolanaBuyerProof(message, buyerAddress, buyerSignature)
	default:
		return fmt.Errorf("unsupported chain: %s", chain)
	}
}

func verifyPolygonBuyerProof(message, buyerAddress, buyerSignature string) error {
	addr := strings.TrimSpace(buyerAddress)
	if !common.IsHexAddress(addr) {
		return fmt.Errorf("invalid polygon buyer_address")
	}

	sigBytes, err := hexutil.Decode(strings.TrimSpace(buyerSignature))
	if err != nil {
		return fmt.Errorf("invalid polygon buyer_signature encoding")
	}
	if len(sigBytes) != 65 {
		return fmt.Errorf("invalid polygon buyer_signature length")
	}

	sig := make([]byte, len(sigBytes))
	copy(sig, sigBytes)
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	if sig[64] > 1 {
		return fmt.Errorf("invalid polygon buyer_signature recovery id")
	}

	msgHash := accounts.TextHash([]byte(message))
	pubkey, err := crypto.SigToPub(msgHash, sig)
	if err != nil {
		return fmt.Errorf("invalid polygon buyer_signature")
	}
	recovered := crypto.PubkeyToAddress(*pubkey)
	expected := common.HexToAddress(addr)
	if recovered != expected {
		return fmt.Errorf("polygon buyer_signature does not match buyer_address")
	}
	return nil
}

func verifySolanaBuyerProof(message, buyerAddress, buyerSignature string) error {
	pubkeyBytes, err := base58.Decode(strings.TrimSpace(buyerAddress))
	if err != nil {
		return fmt.Errorf("invalid solana buyer_address")
	}
	if len(pubkeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid solana buyer_address length")
	}

	sigText := strings.TrimSpace(buyerSignature)
	sigBytes, err := base64.StdEncoding.DecodeString(sigText)
	if err != nil {
		sigBytes, err = base64.RawStdEncoding.DecodeString(sigText)
		if err != nil {
			return fmt.Errorf("invalid solana buyer_signature encoding")
		}
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return fmt.Errorf("invalid solana buyer_signature length")
	}

	if !ed25519.Verify(ed25519.PublicKey(pubkeyBytes), []byte(message), sigBytes) {
		return fmt.Errorf("solana buyer_signature does not match buyer_address")
	}
	return nil
}

func buyerAddressMatches(chain, verifiedAddress, claimedAddress string) bool {
	verified := strings.TrimSpace(verifiedAddress)
	claimed := strings.TrimSpace(claimedAddress)

	switch strings.TrimSpace(chain) {
	case "polygon":
		if common.IsHexAddress(verified) && common.IsHexAddress(claimed) {
			return common.HexToAddress(verified) == common.HexToAddress(claimed)
		}
		return strings.EqualFold(verified, claimed)
	case "solana":
		return verified == claimed
	default:
		return verified == claimed
	}
}

func validatePaywallBlockTime(blockTime, productCreatedAt time.Time) error {
	gracePeriod := 5 * time.Minute
	if blockTime.Before(productCreatedAt.Add(-gracePeriod)) {
		return fmt.Errorf("transaction is older than the product (block time %s, product created %s)",
			blockTime.UTC().Format(time.RFC3339), productCreatedAt.UTC().Format(time.RFC3339))
	}
	return nil
}
