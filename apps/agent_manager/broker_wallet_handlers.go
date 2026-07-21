package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

const defaultPolygonUSDTeTokenAddress = "0xC2132D05D31c914a87C6611C10748AEb04B58e8F"
const defaultPolygonUSDCeTokenAddress = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"

func (h *BrokerWalletHandler) handleGetAddress(w http.ResponseWriter, r *http.Request) {
	walletToolHandler(h, w, r, false, func(wt *tools.WalletTools, input tools.GetWalletAddressInput) (map[string]any, error) {
		result, err := wt.GetWalletAddressTool(r.Context(), input)
		if err != nil {
			return nil, err
		}
		return result.ToMap(), nil
	})
}

func (h *BrokerWalletHandler) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}

	var input tools.GetBalanceInput
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}

	config, companyID, err := h.getWalletConfig(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	normalizedInput := normalizeGetBalanceInput(input)
	if shouldUsePolygonRPCForBalance(input) {
		config.EthRPCURL = h.resolvePolygonRPCURL(r.Context(), companyID)
	}

	walletTools, err := tools.NewWalletToolsWithConfig(config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create wallet tools: "+err.Error())
		return
	}

	result, err := walletTools.GetBalanceTool(r.Context(), normalizedInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	payload := map[string]any{}
	if result != nil {
		payload = result.ToMap()
		if payload == nil {
			payload = map[string]any{}
		}
	}
	if strings.EqualFold(strings.TrimSpace(input.Chain), "polygon") {
		payload["chain"] = "polygon"
	}
	payload["identity_scope"] = "company"
	payload["company_id"] = companyID

	writeJSON(w, http.StatusOK, payload)
}

func (h *BrokerWalletHandler) handleGetBalances(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}

	config, companyID, err := h.getWalletConfig(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	defaultWallet, err := tools.NewWalletToolsWithConfig(config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create wallet tools: "+err.Error())
		return
	}

	solanaResult, solanaErr := defaultWallet.GetBalanceTool(r.Context(), tools.GetBalanceInput{Chain: "solana"})
	ethResult, ethErr := defaultWallet.GetBalanceTool(r.Context(), tools.GetBalanceInput{Chain: "ethereum"})

	polygonRow := map[string]any{"ok": false, "error": "polygon rpc is not configured"}
	polygonUSDTeRow := map[string]any{"ok": false, "error": "polygon rpc is not configured"}
	polygonUSDCeRow := map[string]any{"ok": false, "error": "polygon rpc is not configured"}

	polygonRPCURL := h.resolvePolygonRPCURL(r.Context(), companyID)
	polygonUSDTeToken := polygonUSDTeTokenAddress()
	polygonUSDCeToken := polygonUSDCeTokenAddress()
	polygonUSDTeRow["token_address"] = polygonUSDTeToken
	polygonUSDCeRow["token_address"] = polygonUSDCeToken

	if polygonRPCURL != "" {
		polygonConfig := config
		polygonConfig.EthRPCURL = polygonRPCURL
		polygonWallet, err := tools.NewWalletToolsWithConfig(polygonConfig)
		if err != nil {
			polygonRow = map[string]any{"ok": false, "error": "failed to create polygon wallet tools: " + err.Error()}
			polygonUSDTeRow = map[string]any{
				"ok":            false,
				"error":         "failed to create polygon wallet tools: " + err.Error(),
				"token_address": polygonUSDTeToken,
			}
			polygonUSDCeRow = map[string]any{
				"ok":            false,
				"error":         "failed to create polygon wallet tools: " + err.Error(),
				"token_address": polygonUSDCeToken,
			}
		} else {
			polygonBalanceResult, polygonBalanceErr := polygonWallet.GetBalanceTool(r.Context(), tools.GetBalanceInput{Chain: "ethereum"})
			polygonUSDTeResult, polygonUSDTeErr := polygonWallet.GetBalanceTool(r.Context(), tools.GetBalanceInput{
				Chain:        "ethereum",
				TokenAddress: polygonUSDTeToken,
			})
			polygonUSDCeResult, polygonUSDCeErr := polygonWallet.GetBalanceTool(r.Context(), tools.GetBalanceInput{
				Chain:        "ethereum",
				TokenAddress: polygonUSDCeToken,
			})
			polygonRow = compactBalanceSnapshot(polygonBalanceResult, polygonBalanceErr)
			polygonUSDTeRow = compactBalanceSnapshot(polygonUSDTeResult, polygonUSDTeErr)
			polygonUSDCeRow = compactBalanceSnapshot(polygonUSDCeResult, polygonUSDCeErr)
			polygonRow["chain"] = "polygon"
			polygonUSDTeRow["chain"] = "polygon"
			polygonUSDCeRow["chain"] = "polygon"
			polygonUSDTeRow["token_address"] = polygonUSDTeToken
			polygonUSDCeRow["token_address"] = polygonUSDCeToken
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"solana":         compactBalanceSnapshot(solanaResult, solanaErr),
		"eth":            compactBalanceSnapshot(ethResult, ethErr),
		"polygon":        polygonRow,
		"polygon_usdte":  polygonUSDTeRow,
		"polygon_usdce":  polygonUSDCeRow,
		"identity_scope": "company",
		"company_id":     companyID,
	})
}

func compactBalanceSnapshot(result *loop.ToolResult, callErr error) map[string]any {
	if callErr != nil {
		return map[string]any{"ok": false, "error": callErr.Error()}
	}
	if result == nil {
		return map[string]any{"ok": false, "error": "empty balance result"}
	}
	if !result.Success {
		msg := strings.TrimSpace(result.Error)
		if msg == "" {
			msg = "balance check failed"
		}
		return map[string]any{"ok": false, "error": msg}
	}

	content, ok := result.Content.(map[string]any)
	if !ok {
		return map[string]any{"ok": false, "error": "unexpected balance payload"}
	}

	out := map[string]any{"ok": true}
	for _, key := range []string{"chain", "address", "balance", "balance_raw", "symbol", "decimals", "balance_type", "token_address"} {
		if v, ok := content[key]; ok && v != nil {
			out[key] = v
		}
	}
	return out
}

func (h *BrokerWalletHandler) handleSign(w http.ResponseWriter, r *http.Request) {
	walletToolHandler(h, w, r, false, func(wt *tools.WalletTools, input tools.SignMessageInput) (map[string]any, error) {
		result, err := wt.SignMessageTool(r.Context(), input)
		if err != nil {
			return nil, err
		}
		return result.ToMap(), nil
	})
}

func (h *BrokerWalletHandler) handleSend(w http.ResponseWriter, r *http.Request) {
	walletToolHandler(h, w, r, true, func(wt *tools.WalletTools, input tools.SendTokenInput) (map[string]any, error) {
		result, err := wt.SendTokenTool(r.Context(), input)
		if err != nil {
			return nil, err
		}
		return result.ToMap(), nil
	})
}

func (h *BrokerWalletHandler) handleSwap(w http.ResponseWriter, r *http.Request) {
	walletToolHandler(h, w, r, true, func(wt *tools.WalletTools, input tools.SwapTokenInput) (map[string]any, error) {
		result, err := wt.SwapTokenTool(r.Context(), input)
		if err != nil {
			return nil, err
		}
		return result.ToMap(), nil
	})
}

func (h *BrokerWalletHandler) handleContract(w http.ResponseWriter, r *http.Request) {
	walletToolHandler(h, w, r, true, func(wt *tools.WalletTools, input tools.ContractCallInput) (map[string]any, error) {
		result, err := wt.ContractCallTool(r.Context(), input)
		if err != nil {
			return nil, err
		}
		return result.ToMap(), nil
	})
}

func (h *BrokerWalletHandler) handleHistory(w http.ResponseWriter, r *http.Request) {
	walletToolHandler(h, w, r, false, func(wt *tools.WalletTools, input tools.GetTransactionHistoryInput) (map[string]any, error) {
		result, err := wt.GetTransactionHistoryTool(r.Context(), input)
		if err != nil {
			return nil, err
		}
		return result.ToMap(), nil
	})
}

func (h *BrokerWalletHandler) handleEncrypt(w http.ResponseWriter, r *http.Request) {
	walletToolHandler(h, w, r, false, func(wt *tools.WalletTools, input tools.EncryptMessageInput) (map[string]any, error) {
		result, err := wt.EncryptMessageTool(r.Context(), input)
		if err != nil {
			return nil, err
		}
		return result.ToMap(), nil
	})
}

func (h *BrokerWalletHandler) handleDecrypt(w http.ResponseWriter, r *http.Request) {
	walletToolHandler(h, w, r, false, func(wt *tools.WalletTools, input tools.DecryptMessageInput) (map[string]any, error) {
		result, err := wt.DecryptMessageTool(r.Context(), input)
		if err != nil {
			return nil, err
		}
		return result.ToMap(), nil
	})
}

func (h *BrokerWalletHandler) handlePubKey(w http.ResponseWriter, r *http.Request) {
	walletToolHandler(h, w, r, false, func(wt *tools.WalletTools, input tools.GetEd25519PublicKeyInput) (map[string]any, error) {
		result, err := wt.GetEd25519PublicKeyTool(r.Context(), input)
		if err != nil {
			return nil, err
		}
		return result.ToMap(), nil
	})
}
