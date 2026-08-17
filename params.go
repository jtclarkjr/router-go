package router

import (
	"context"
	"net/http"
)

type contextKey string

func requestWithParams(req *http.Request, keys, matches []string) *http.Request {
	ctx := req.Context()
	for i, key := range keys {
		ctx = context.WithValue(ctx, contextKey(key), matches[i+1])
	}

	// Clone copies the standard library's internal path-value storage. A
	// shallow WithContext copy would let SetPathValue mutate values installed
	// by an outer router on the original request.
	requestWithContext := req.Clone(ctx)
	for i, key := range keys {
		requestWithContext.SetPathValue(key, matches[i+1])
	}
	return requestWithContext
}

// URLParam retrieves a path parameter from the request context.
func URLParam(r *http.Request, key string) string {
	if value := r.PathValue(key); value != "" {
		return value
	}
	if value, ok := r.Context().Value(contextKey(key)).(string); ok {
		return value
	}
	return ""
}

// URLQuery retrieves the first query value associated with key.
func URLQuery(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}
