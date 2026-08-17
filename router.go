package router

import (
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Middleware wraps an HTTP handler with additional behavior.
type Middleware func(http.Handler) http.Handler

// Router maps HTTP methods and paths to handlers.
//
// A Router is safe for concurrent request dispatch and route registration.
// Middleware is captured when each route is registered, preserving the
// package's registration-time middleware semantics.
// A Router must not be copied after first use.
type Router struct {
	mu           sync.RWMutex
	routes       map[string]map[string]Route
	patterns     map[string]compiledRoute
	signatures   map[string]string
	orderedPaths []string
	middleware   []Middleware
}

// NewRouter creates an initialized Router.
func NewRouter() *Router {
	return &Router{
		routes:     make(map[string]map[string]Route),
		patterns:   make(map[string]compiledRoute),
		signatures: make(map[string]string),
		middleware: []Middleware{},
	}
}

// Use appends middleware for routes registered after this call.
func (r *Router) Use(mw Middleware) {
	if mw == nil {
		return
	}

	r.mu.Lock()
	r.middleware = append(r.middleware, mw)
	r.mu.Unlock()
}

// ServeHTTP dispatches a request using deterministic route precedence:
// static segments before parameters, parameters before wildcards, and longer
// wildcard prefixes before shorter ones.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	route, _, matches, allowed, pathFound := r.match(req.Method, req.URL.Path)
	if !pathFound {
		http.NotFound(w, req)
		return
	}
	if route.Handler == nil {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	route.Handler.ServeHTTP(w, requestWithParams(req, route.ParamKeys, matches))
}

func (r *Router) match(method, requestPath string) (Route, compiledRoute, []string, []string, bool) {
	method = strings.ToUpper(method)

	r.mu.RLock()
	defer r.mu.RUnlock()

	selectedSignature := ""
	var selectedPattern compiledRoute
	var selectedMatches []string
	allowedSet := make(map[string]bool)

	for _, path := range r.orderedPaths {
		pattern := r.patterns[path]
		matches := pattern.pattern.FindStringSubmatch(requestPath)
		if matches == nil {
			continue
		}
		if selectedSignature == "" {
			selectedSignature = pattern.signature
			selectedPattern = pattern
			selectedMatches = matches
		}
		// Structurally equivalent paths may use different parameter names for
		// different methods. They form one precedence tier; lower-precedence
		// patterns must not become a method fallback for a more-specific path.
		if pattern.signature != selectedSignature {
			continue
		}

		methods := r.routes[path]
		if route, ok := methods[method]; ok {
			return route, pattern, matches, nil, true
		}

		for registeredMethod := range methods {
			allowedSet[registeredMethod] = true
		}
	}
	if selectedSignature != "" {
		allowed := make([]string, 0, len(allowedSet))
		for registeredMethod := range allowedSet {
			allowed = append(allowed, registeredMethod)
		}
		sort.Strings(allowed)
		return Route{}, selectedPattern, selectedMatches, allowed, true
	}

	return Route{}, compiledRoute{}, nil, nil, false
}
