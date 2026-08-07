package router

import (
	"net/http"
	"strings"
)

// Middleware wraps an HTTP handler with additional behavior.
type Middleware func(http.Handler) http.Handler

// Router maps HTTP methods and paths to handlers.
type Router struct {
	routes     map[string]map[string]Route
	middleware []Middleware
}

// NewRouter creates an initialized Router.
func NewRouter() *Router {
	return &Router{
		routes:     make(map[string]map[string]Route),
		middleware: []Middleware{},
	}
}

// Use appends middleware for routes registered after this call.
func (r *Router) Use(mw Middleware) {
	r.middleware = append(r.middleware, mw)
}

// ServeHTTP dispatches the request to the first matching route.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	for path, methods := range r.routes {
		route, ok := methods[req.Method]
		if !ok {
			continue
		}

		if serveWildcardRoute(w, req, path, route) {
			return
		}
		if servePatternRoute(w, req, route) {
			return
		}
	}

	http.NotFound(w, req)
}

func serveWildcardRoute(w http.ResponseWriter, req *http.Request, path string, route Route) bool {
	if !strings.HasSuffix(path, "/*") {
		return false
	}

	prefix := strings.TrimSuffix(path, "/*")
	if !strings.HasPrefix(req.URL.Path, prefix) {
		return false
	}

	route.Handler.ServeHTTP(w, req)
	return true
}

func servePatternRoute(w http.ResponseWriter, req *http.Request, route Route) bool {
	matches := route.ParamPattern.FindStringSubmatch(req.URL.Path)
	if matches == nil {
		return false
	}

	route.Handler.ServeHTTP(w, requestWithParams(req, route.ParamKeys, matches))
	return true
}
