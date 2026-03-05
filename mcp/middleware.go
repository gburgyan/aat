package mcp

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// AuthMiddleware returns an HTTP middleware that validates Bearer tokens.
// Requests without a valid Authorization header receive a 401 response
// before reaching the MCP server.
//
// Usage with ServeHTTP is not automatic — this middleware is provided as a
// building block. To wire it in, wrap the StreamableHTTPServer handler:
//
//	handler := server.NewStreamableHTTPServer(mcpServer, opts...)
//	protected := mcp.AuthMiddleware([]string{"secret-key"})(handler)
//	http.ListenAndServe(":8080", protected)
//
// A future --api-key CLI flag can use this directly.
func AuthMiddleware(keys []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, "missing or malformed Authorization header", http.StatusUnauthorized)
				return
			}

			if !validateToken(token, keys) {
				http.Error(w, "invalid API key", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractBearerToken extracts the token from an "Authorization: Bearer <token>" header.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return auth[len(prefix):]
}

// validateToken checks the token against the allowed keys using constant-time comparison.
func validateToken(token string, keys []string) bool {
	for _, key := range keys {
		if subtle.ConstantTimeCompare([]byte(token), []byte(key)) == 1 {
			return true
		}
	}
	return false
}
