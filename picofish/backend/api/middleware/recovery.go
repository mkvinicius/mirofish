// Package middleware provides HTTP middleware for PicoFish.
package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/google/uuid"
)

// Recovery wraps any HTTP handler with panic recovery.
// On panic it logs the stack trace and returns a structured JSON error
// so the server keeps running rather than crashing.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				requestID := uuid.NewString()
				fmt.Printf("[PANIC] requestID=%s method=%s path=%s err=%v\n%s\n",
					requestID, r.Method, r.URL.Path, rec, debug.Stack())
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":      "internal error — server recovered",
					"request_id": requestID,
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
