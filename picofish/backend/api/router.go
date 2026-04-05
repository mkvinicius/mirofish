// Package api provides a lightweight HTTP router using only net/http.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Router is a minimal HTTP router with pattern matching and CORS.
type Router struct {
	mux *http.ServeMux
}

func NewRouter() *Router {
	return &Router{mux: http.NewServeMux()}
}

// Handle registers a handler with method+path routing.
// Uses Go 1.22+ method-qualified patterns ("GET /path") to avoid duplicate registrations.
// OPTIONS is handled globally in ServeHTTP before the mux.
func (r *Router) Handle(method, path string, h http.HandlerFunc) {
	r.mux.HandleFunc(method+" "+path, func(w http.ResponseWriter, req *http.Request) {
		setCORS(w)
		h(w, req)
	})
}

// ServeHTTP implements http.Handler with global CORS preflight support.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	setCORS(w)
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	r.mux.ServeHTTP(w, req)
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// JSON writes a JSON response.
func JSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Err writes a JSON error response.
func Err(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// Param extracts a path segment at position n (0-indexed after prefix).
// e.g. for "/api/v1/projects/abc/graph", prefix="/api/v1/projects/", n=0 → "abc", n=1 → "graph"
func Param(r *http.Request, prefix string, n int) string {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if n >= len(parts) {
		return ""
	}
	return parts[n]
}
