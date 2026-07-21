package server

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
)

// ExtractAgentIDMiddleware extracts and validates the agent ID header.
func ExtractAgentIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentID := r.Header.Get(HeaderAgentID)
		if agentID == "" {
			writeBadRequest(w, "Missing X-Agent-ID header")
			return
		}

		// Validate format (Base64URL, 43 chars for 32 bytes)
		if len(agentID) != 43 {
			writeBadRequest(w, "Invalid X-Agent-ID: expected 43 character Base64URL encoded Ed25519 public key")
			return
		}

		// Decode to verify it's valid
		pubkey, err := gowild_agent_net.DecodePublicKey(agentID)
		if err != nil {
			writeBadRequest(w, "Invalid X-Agent-ID: "+err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyAgentID, agentID)
		ctx = context.WithValue(ctx, ctxKeyPublicKey, pubkey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RevokedKeyMiddleware checks if the agent's key has been revoked.
func RevokedKeyMiddleware(service *gowild_agent_net.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agentID := GetAgentID(r.Context())

			isRevoked, err := service.IsKeyRevoked(r.Context(), agentID)
			if err != nil {
				writeInternalError(w, "Failed to check key status")
				return
			}

			if isRevoked {
				writeForbidden(w, "This key has been revoked. Generate a new keypair to continue.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// TimestampMiddleware validates the request timestamp.
func TimestampMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestampStr := r.Header.Get(HeaderAgentTimestamp)
		if timestampStr == "" {
			writeTimestampError(w, "Missing X-Agent-Timestamp header")
			return
		}

		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			writeTimestampError(w, "X-Agent-Timestamp must be ISO8601/RFC3339 format")
			return
		}

		now := time.Now()
		diff := now.Sub(timestamp)
		if diff < 0 {
			diff = -diff
		}

		if diff > TimestampTolerance {
			writeTimestampError(w, "X-Agent-Timestamp must be within ±5 minutes of server time")
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyTimestamp, timestamp)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SignatureMiddleware verifies the request signature.
func SignatureMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigStr := r.Header.Get(HeaderAgentSig)
		if sigStr == "" {
			writeUnauthorized(w, "Missing X-Agent-Sig header")
			return
		}

		// Decode signature (86 chars for 64 bytes Base64URL)
		if len(sigStr) != 86 {
			writeUnauthorized(w, "Invalid X-Agent-Sig: expected 86 character Base64URL encoded signature")
			return
		}

		sig, err := gowild_agent_net.DecodeSignature(sigStr)
		if err != nil {
			writeUnauthorized(w, "Invalid X-Agent-Sig: "+err.Error())
			return
		}

		// Get public key from context
		pubkey, ok := r.Context().Value(ctxKeyPublicKey).(ed25519.PublicKey)
		if !ok {
			writeInternalError(w, "Public key not in context")
			return
		}

		// Get timestamp
		timestampStr := r.Header.Get(HeaderAgentTimestamp)

		// Get cached body
		body := GetBody(r.Context())

		// Verify signature
		if !gowild_agent_net.VerifySignature(pubkey, r.Method, r.URL.Path, timestampStr, body, sig) {
			writeSignatureError(w, r.Method, r.URL.Path, timestampStr)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// TierLookupMiddleware determines the agent's tier.
func TierLookupMiddleware(service *gowild_agent_net.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agentID := GetAgentID(r.Context())

			tier, err := service.GetAgentTier(r.Context(), agentID)
			if err != nil {
				writeInternalError(w, "Failed to determine agent tier")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyAgentTier, tier)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PoWOrPremiumMiddleware verifies PoW for non-premium agents.
func PoWOrPremiumMiddleware(service *gowild_agent_net.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tier := GetAgentTier(r.Context())

			// Premium agents skip PoW
			if tier == gowild_agent_net.TierPremium {
				next.ServeHTTP(w, r)
				return
			}

			// Free tier must provide valid PoW
			powHash := r.Header.Get(HeaderAgentPoW)
			nonce := r.Header.Get(HeaderAgentNonce)

			if powHash == "" || nonce == "" {
				difficulty, _ := service.GetCurrentDifficulty(r.Context())
				writePaymentRequired(w, "Include a valid Proof of Work header OR upgrade to Premium.", difficulty.CurrentDifficulty)
				return
			}

			// Validate nonce format
			if err := gowild_agent_net.ValidateNonce(nonce); err != nil {
				writeBadRequest(w, "Invalid X-Agent-Nonce: "+err.Error())
				return
			}

			// Validate PoW hash format (64 hex chars)
			if len(powHash) != 64 {
				writeBadRequest(w, "Invalid X-Agent-PoW: expected 64 character hex string")
				return
			}

			// Get canonical JSON of body for PoW verification
			body := GetBody(r.Context())
			timestampStr := r.Header.Get(HeaderAgentTimestamp)

			// Canonicalize JSON if body is present
			var payloadJSON []byte
			if len(body) > 0 {
				var parsed any
				if err := json.Unmarshal(body, &parsed); err == nil {
					payloadJSON, err = gowild_agent_net.CanonicalJSON(parsed)
					if err != nil {
						payloadJSON = body // Fallback to raw body
					}
				} else {
					payloadJSON = body
				}
			}

			// Verify PoW with detailed logging
			difficulty, _ := service.GetCurrentDifficulty(r.Context())

			// Compute expected hash for logging
			challenge := gowild_agent_net.ComputePoWChallenge(payloadJSON, timestampStr, nonce)
			expectedHash := gowild_agent_net.ComputePoWHash(challenge)
			expectedHashHex := fmt.Sprintf("%x", expectedHash)
			leadingZeros := gowild_agent_net.CountLeadingZeroBits(expectedHash)

			valid, err := service.VerifyPoW(powHash, payloadJSON, timestampStr, nonce)
			if err != nil {
				log.Printf("PoW FAILED for agent=%s: %v", GetAgentID(r.Context()), err)
				writeBadRequest(w, "PoW verification failed: "+err.Error())
				return
			}

			if !valid {
				hashMatches := (powHash == expectedHashHex)
				log.Printf("PoW INVALID for agent=%s:", GetAgentID(r.Context()))
				log.Printf("  canonical_json: %s", string(payloadJSON))
				log.Printf("  timestamp: %s", timestampStr)
				log.Printf("  nonce: %s", nonce)
				log.Printf("  provided_hash: %s", powHash)
				log.Printf("  expected_hash: %s", expectedHashHex)
				log.Printf("  hash_matches: %v", hashMatches)
				log.Printf("  leading_zeros: %d (required: %d)", leadingZeros, difficulty.CurrentDifficulty)
				writeInvalidPoW(w, "PoW hash does not meet difficulty requirement or is invalid", difficulty.CurrentDifficulty)
				return
			}

			log.Printf("PoW OK for agent=%s (zeros=%d)", GetAgentID(r.Context()), leadingZeros)

			next.ServeHTTP(w, r)
		})
	}
}

// NonceMiddleware checks for replay attacks via nonce tracking.
func NonceMiddleware(service *gowild_agent_net.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tier := GetAgentTier(r.Context())
			agentID := GetAgentID(r.Context())
			timestamp := GetTimestamp(r.Context())

			// Get nonce - required for free tier, optional for premium
			nonce := r.Header.Get(HeaderAgentNonce)
			if tier == gowild_agent_net.TierFree && nonce == "" {
				// Already checked in PoWOrPremiumMiddleware
				next.ServeHTTP(w, r)
				return
			}

			if nonce != "" {
				// Check and record nonce
				if err := service.CheckNonce(r.Context(), agentID, nonce, timestamp); err != nil {
					writeReplayDetected(w, "Nonce already used: possible replay attack")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitMiddleware enforces rate limits.
func RateLimitMiddleware(service *gowild_agent_net.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agentID := GetAgentID(r.Context())
			tier := GetAgentTier(r.Context())

			if err := service.CheckRateLimit(r.Context(), agentID, tier); err != nil {
				if rateLimitErr, ok := err.(*gowild_agent_net.RateLimitError); ok {
					limit := "1/min"
					if tier == gowild_agent_net.TierPremium {
						limit = "60/min"
					}
					writeRateLimited(w, "Rate limit exceeded.", rateLimitErr.RetryAfterSeconds(), limit)
					return
				}
				writeInternalError(w, "Failed to check rate limit")
				return
			}

			// Record this request
			if err := service.RecordRequest(r.Context(), agentID); err != nil {
				// Log but don't fail the request
				log.Printf("Failed to record request for rate limiting: %v", err)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// PremiumOnlyMiddleware rejects non-premium agents with 402.
func PremiumOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tier := GetAgentTier(r.Context())
		if tier != gowild_agent_net.TierPremium {
			writeError(w, http.StatusPaymentRequired, ErrorResponse{
				Error:   ErrCodePremiumRequired,
				Message: "This feature requires a premium account.",
				UpgradeInfo: &UpgradeInfo{
					TreasuryEndpoint: "/api/v1/treasury",
					RequiredAmounts: map[string]string{
						"solana":   "0.005 SOL",
						"ethereum": "0.005 ETH",
					},
				},
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
