package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// LoggingMiddleware logs incoming requests.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		log.Printf("%s %s %d %v agent=%s",
			r.Method, r.URL.Path, wrapped.status, time.Since(start),
			r.Header.Get(HeaderAgentID))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// RecoveryMiddleware recovers from panics.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic recovered: %v", err)
				writeInternalError(w, "Internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// BodyCacheMiddleware caches the request body for signature verification.
func BodyCacheMiddleware(next http.Handler) http.Handler {
	return bodyCacheMiddleware(MaxBodySize)(next)
}

// LargeBodyCacheMiddleware caches the request body with a 50MB limit (for file uploads).
func LargeBodyCacheMiddleware(next http.Handler) http.Handler {
	return bodyCacheMiddleware(50 << 20)(next)
}

func bodyCacheMiddleware(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)

			body, err := io.ReadAll(r.Body)
			if err != nil {
				if strings.Contains(err.Error(), "request body too large") {
					writeBadRequest(w, fmt.Sprintf("Request body too large (max %d MB)", maxSize>>20))
					return
				}
				writeBadRequest(w, "Failed to read request body")
				return
			}
			r.Body.Close()

			r.Body = io.NopCloser(bytes.NewReader(body))

			ctx := context.WithValue(r.Context(), ctxKeyBody, body)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
