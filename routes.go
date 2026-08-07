package router

import (
	"net/http"
	"regexp"

	"github.com/jtclarkjr/router-go/middleware"
)

var routeParamPattern = regexp.MustCompile(`\{(\w+)\}`)

// Route stores a registered handler and its compiled path-parameter metadata.
type Route struct {
	Handler      http.Handler
	ParamKeys    []string
	ParamPattern *regexp.Regexp
}

// Route registers routes from a temporary subrouter under pathPrefix.
func (r *Router) Route(pathPrefix string, fn func(router *Router)) {
	subrouter := &Router{
		routes:     make(map[string]map[string]Route),
		middleware: make([]Middleware, len(r.middleware)),
	}
	copy(subrouter.middleware, r.middleware)

	fn(subrouter)

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

// Handle registers handler for method and path.
func (r *Router) Handle(method, path string, handler http.Handler) {
	handler = applyMiddleware(handler, r.middleware)
	paramKeys, compiledPattern := compileRoutePattern(path)

	if r.routes[path] == nil {
		r.routes[path] = make(map[string]Route)
	}
	r.routes[path][method] = Route{
		Handler:      handler,
		ParamKeys:    paramKeys,
		ParamPattern: compiledPattern,
	}
}

func applyMiddleware(handler http.Handler, middleware []Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

func compileRoutePattern(path string) ([]string, *regexp.Regexp) {
	matches := routeParamPattern.FindAllStringSubmatch(path, -1)
	paramKeys := make([]string, 0, len(matches))
	for _, match := range matches {
		paramKeys = append(paramKeys, match[1])
	}

	regexPath := "^" + routeParamPattern.ReplaceAllString(path, `([^/]+)`) + "$"
	return paramKeys, regexp.MustCompile(regexPath)
}

// Get registers a GET handler for path.
func (r *Router) Get(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodGet, path, handler)
}

// Post registers a POST handler for path.
func (r *Router) Post(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodPost, path, handler)
}

// Put registers a PUT handler for path.
func (r *Router) Put(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodPut, path, handler)
}

// Patch registers a PATCH handler for path.
func (r *Router) Patch(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodPatch, path, handler)
}

// Delete registers a DELETE handler for path.
func (r *Router) Delete(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodDelete, path, handler)
}

// Head registers a HEAD handler for path.
func (r *Router) Head(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodHead, path, handler)
}

// Options registers an OPTIONS handler for path.
func (r *Router) Options(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodOptions, path, handler)
}

// Connect registers a CONNECT handler for path.
func (r *Router) Connect(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodConnect, path, handler)
}

// Trace registers a TRACE handler for path.
func (r *Router) Trace(path string, handler http.HandlerFunc) {
	r.Handle(http.MethodTrace, path, handler)
}

// WS registers a GET route that upgrades to a WebSocket connection.
func (r *Router) WS(path string, handler middleware.WSHandler) {
	wsMiddleware := middleware.WebSocket(handler)
	r.Handle(http.MethodGet, path, wsMiddleware(http.NotFoundHandler()))
}
