package server

import (
	"net/http"

	"github.com/original-david-knight/go_wild/agent_net"
)

// Chain applies middleware in order (outermost first).
func Chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

// AuthChain returns the full authentication middleware chain.
func AuthChain(service *gowild_agent_net.Service, h http.Handler) http.Handler {
	return Chain(h,
		LoggingMiddleware,
		RecoveryMiddleware,
		BodyCacheMiddleware,
		ExtractAgentIDMiddleware,
		RevokedKeyMiddleware(service),
		TimestampMiddleware,
		SignatureMiddleware,
		TierLookupMiddleware(service),
		PoWOrPremiumMiddleware(service),
		NonceMiddleware(service),
		RateLimitMiddleware(service),
	)
}

// SignatureOnlyChain returns a lighter auth chain (no PoW/rate limit).
func SignatureOnlyChain(service *gowild_agent_net.Service, h http.Handler) http.Handler {
	return Chain(h,
		LoggingMiddleware,
		RecoveryMiddleware,
		BodyCacheMiddleware,
		ExtractAgentIDMiddleware,
		RevokedKeyMiddleware(service),
		TimestampMiddleware,
		SignatureMiddleware,
		TierLookupMiddleware(service),
	)
}

// PremiumAuthChain returns the full authentication middleware chain for premium-only endpoints.
// Same as AuthChain but replaces PoWOrPremiumMiddleware with PremiumOnlyMiddleware.
func PremiumAuthChain(service *gowild_agent_net.Service, h http.Handler) http.Handler {
	return Chain(h,
		LoggingMiddleware,
		RecoveryMiddleware,
		BodyCacheMiddleware,
		ExtractAgentIDMiddleware,
		RevokedKeyMiddleware(service),
		TimestampMiddleware,
		SignatureMiddleware,
		TierLookupMiddleware(service),
		PremiumOnlyMiddleware,
		NonceMiddleware(service),
		RateLimitMiddleware(service),
	)
}

// PremiumAuthChainLargeBody is like PremiumAuthChain but allows up to 50MB request bodies (for file uploads).
func PremiumAuthChainLargeBody(service *gowild_agent_net.Service, h http.Handler) http.Handler {
	return Chain(h,
		LoggingMiddleware,
		RecoveryMiddleware,
		LargeBodyCacheMiddleware,
		ExtractAgentIDMiddleware,
		RevokedKeyMiddleware(service),
		TimestampMiddleware,
		SignatureMiddleware,
		TierLookupMiddleware(service),
		PremiumOnlyMiddleware,
		NonceMiddleware(service),
		RateLimitMiddleware(service),
	)
}

// PublicChain returns middleware for public endpoints (no auth).
func PublicChain(h http.Handler) http.Handler {
	return Chain(h,
		LoggingMiddleware,
		RecoveryMiddleware,
	)
}
