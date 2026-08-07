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
	return req.WithContext(ctx)
}

// URLParam retrieves a path parameter from the request context.
func URLParam(r *http.Request, key string) string {
	if value, ok := r.Context().Value(contextKey(key)).(string); ok {
		return value
	}
	return ""
}

// URLQuery retrieves the first query value associated with key.
func URLQuery(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}
