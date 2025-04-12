package router

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

// Middleware represents a function that wraps an http.Handler
type Middleware func(http.Handler) http.Handler

// Route stores information about a route, including its handler and parameter keys
type Route struct {
	Handler      http.Handler
	ParamKeys    []string
	ParamPattern *regexp.Regexp
}

// Router is a custom router that maps methods and paths to handlers
type Router struct {
	routes     map[string]map[string]Route
	middleware []Middleware
}

// NewRouter creates a new Router instance
func NewRouter() *Router {
	return &Router{
		routes:     make(map[string]map[string]Route),
		middleware: []Middleware{},
	}
}

// Route creates a subrouter for the given path prefix
func (r *Router) Route(pathPrefix string, fn func(router *Router)) {
	// Create a new subrouter
	subrouter := &Router{
		routes:     make(map[string]map[string]Route),
		middleware: make([]Middleware, len(r.middleware)),
	}

	// Copy parent middleware
	copy(subrouter.middleware, r.middleware)

	// Execute the routing function on the subrouter
	fn(subrouter)

	// For each route in the subrouter, add it to the parent router with the prefix
	for path, methods := range subrouter.routes {
		fullPath := pathPrefix + path
		for method, route := range methods {
			if r.routes[fullPath] == nil {
				r.routes[fullPath] = make(map[string]Route)
			}
			r.routes[fullPath][method] = route
		}
	}
}

// Use adds a middleware to the router
func (r *Router) Use(mw Middleware) {
	r.middleware = append(r.middleware, mw)
}

// Handle registers a handler for a specific method and path
func (r *Router) Handle(method, path string, handler http.Handler) {
	// Apply middleware to the handler
	for i := len(r.middleware) - 1; i >= 0; i-- {
		handler = r.middleware[i](handler)
	}

	// Extract parameter keys from the path
	paramKeys := []string{}
	paramPattern := regexp.MustCompile(`\{(\w+)\}`)
	matches := paramPattern.FindAllStringSubmatch(path, -1)
	for _, match := range matches {
		paramKeys = append(paramKeys, match[1])
	}

	// Replace parameter placeholders with regex patterns
	regexPath := "^" + paramPattern.ReplaceAllString(path, `([^/]+)`) + "$"
	compiledPattern := regexp.MustCompile(regexPath)

	if r.routes[path] == nil {
		r.routes[path] = make(map[string]Route)
	}
	r.routes[path][method] = Route{
		Handler:      handler,
		ParamKeys:    paramKeys,
		ParamPattern: compiledPattern,
	}
}

// Get registers a GET handler for a specific path
func (r *Router) Get(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodGet, path, handler)
}

// Post registers a POST handler for a specific path
func (r *Router) Post(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodPost, path, handler)
}

// Put registers a PUT handler for a specific path
func (r *Router) Put(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodPut, path, handler)
}

// Put registers a PATCH handler for a specific path
func (r *Router) Patch(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodPatch, path, handler)
}

// Delete registers a DELETE handler for a specific path
func (r *Router) Delete(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodDelete, path, handler)
}

// Head registers a HEAD handler for a specific path
func (r *Router) Head(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodHead, path, handler)
}

// Options registers an OPTIONS handler for a specific path
func (r *Router) Options(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodOptions, path, handler)
}

// Connect registers a CONNECT handler for a specific path
func (r *Router) Connect(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodConnect, path, handler)
}

// Trace registers a TRACE handler for a specific path
func (r *Router) Trace(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodTrace, path, handler)
}

// ServeHTTP implements the http.Handler interface
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	for path, methods := range r.routes {
		for method, route := range methods {
			// Match exact paths or wildcard paths
			if req.Method == method && (route.ParamPattern.MatchString(req.URL.Path) || strings.HasPrefix(req.URL.Path, strings.TrimSuffix(path, "*"))) {
				// Extract parameters from the URL
				matches := route.ParamPattern.FindStringSubmatch(req.URL.Path)
				params := map[string]string{}
				for i, key := range route.ParamKeys {
					params[key] = matches[i+1]
				}

				// Add parameters to the request context
				ctx := req.Context()
				for key, value := range params {
					ctx = context.WithValue(ctx, key, value)
				}
				req = req.WithContext(ctx)

				// Serve the request
				route.Handler.ServeHTTP(w, req)
				return
			}
		}
	}
	http.NotFound(w, req)
}

// URLParam retrieves a URL parameter from the request context
func URLParam(r *http.Request, key string) string {
	if value, ok := r.Context().Value(key).(string); ok {
		return value
	}
	return ""
}

// URLQuery retrieves a query parameter from the URL
func URLQuery(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}
