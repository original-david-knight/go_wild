package server

import (
	"strings"
	"testing"
)

func TestCheckoutHTMLHasRetryVerificationMode(t *testing.T) {
	if !strings.Contains(checkoutHTML, `data-mode="pay"`) {
		t.Fatalf("expected pay button to include data-mode")
	}
	if !strings.Contains(checkoutHTML, "Retry Verification") {
		t.Fatalf("expected checkout HTML to expose retry verification label")
	}
	if strings.Contains(checkoutHTML, "Retry Payment") {
		t.Fatalf("checkout HTML should not show legacy retry payment label")
	}
}

func TestCheckoutHTMLRetryBranchDoesNotResendPayment(t *testing.T) {
	branchStart := strings.Index(checkoutHTML, `if (payBtn.getAttribute('data-mode') === 'verify') {`)
	if branchStart == -1 {
		t.Fatalf("missing verification retry branch")
	}

	// The retry branch should execute before any new transfer setup.
	txInitStart := strings.Index(checkoutHTML, "var txHash;")
	if txInitStart == -1 || txInitStart <= branchStart {
		t.Fatalf("could not locate tx initialization after retry branch")
	}

	retryBranch := checkoutHTML[branchStart:txInitStart]
	if !strings.Contains(retryBranch, "verifyPayment(lastPaidTxHash, lastBuyerSignature)") {
		t.Fatalf("retry branch should reuse existing tx hash and signature for verification")
	}
	if strings.Contains(retryBranch, "contract.transfer(") {
		t.Fatalf("retry branch must not create a new EVM transfer")
	}
	if strings.Contains(retryBranch, "sendRawTransaction(") {
		t.Fatalf("retry branch must not send a new Solana transaction")
	}
}
