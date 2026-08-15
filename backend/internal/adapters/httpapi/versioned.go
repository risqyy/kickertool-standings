package httpapi

import (
	"net/http"
	"strings"
)

// StripV1Prefix lets the JSON handlers keep one routing contract while the
// public HTTP surface is versioned as /api/v1/.... It deliberately forwards
// only that exact prefix and never changes query parameters or methods.
func StripV1Prefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			http.NotFound(w, r)
			return
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = "/api/" + strings.TrimPrefix(r.URL.Path, "/api/v1/")
		next.ServeHTTP(w, clone)
	})
}
