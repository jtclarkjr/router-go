package router

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/jtclarkjr/router-go/middleware"
)

var (
	routeParamPattern = regexp.MustCompile(`\{(\w+)\}`)

	// ErrInvalidRoute indicates that a method, path, or handler cannot be
	// registered.
	ErrInvalidRoute = errors.New("invalid route")
	// ErrDuplicateRoute indicates that a method/path pattern is already
	// registered. Parameter names do not make otherwise identical patterns
	// distinct.
	ErrDuplicateRoute = errors.New("duplicate route")
	// ErrRouteNotFound indicates that a route referenced by another
	// registration, such as an alias target, does not exist.
	ErrRouteNotFound = errors.New("route not found")
)

// RegistrationError describes a failed route registration.
type RegistrationError struct {
	Method       string
	Path         string
	ConflictPath string
	Err          error
}

func (e *RegistrationError) Error() string {
	if e.ConflictPath != "" {
		return fmt.Sprintf("router: %s %s conflicts with %s: %v", e.Method, e.Path, e.ConflictPath, e.Err)
	}
	return fmt.Sprintf("router: %s %s: %v", e.Method, e.Path, e.Err)
}

// Unwrap supports errors.Is with ErrInvalidRoute and ErrDuplicateRoute.
func (e *RegistrationError) Unwrap() error { return e.Err }

// Route stores a registered handler and its compiled path-parameter metadata.
// Existing fields remain exported for source compatibility.
type Route struct {
	Handler      http.Handler
	ParamKeys    []string
	ParamPattern *regexp.Regexp
}

// RouteInfo describes a registered route without exposing its handler.
type RouteInfo struct {
	Method    string
	Path      string
	ParamKeys []string
	Wildcard  bool
}

type routeKind uint8

const (
	staticRoute routeKind = iota
	parameterRoute
	wildcardRoute
)

type compiledRoute struct {
	path            string
	signature       string
	paramKeys       []string
	pattern         *regexp.Regexp
	kind            routeKind
	segmentKinds    []routeKind
	segmentLiterals []int
	segmentParams   []int
	wildcardBase    string
}

type routeRegistration struct {
	method string
	path   string
	route  Route
}

// Route registers routes from a temporary subrouter under pathPrefix.
func (r *Router) Route(pathPrefix string, fn func(router *Router)) {
	if fn == nil {
		return
	}

	subrouter := NewRouter()
	subrouter.middleware = r.middlewareSnapshot()
	fn(subrouter)

	for _, registration := range subrouter.registrations() {
		fullPath := joinRoutePath(pathPrefix, registration.path)
		if err := r.registerPrepared(registration.method, fullPath, registration.route.Handler); err != nil {
			panic(err)
		}
	}
}

func joinRoutePath(prefix, path string) string {
	if prefix == "" {
		return path
	}
	if path == "" {
		return prefix
	}
	if strings.HasSuffix(prefix, "/") && strings.HasPrefix(path, "/") {
		return prefix + strings.TrimPrefix(path, "/")
	}
	if !strings.HasSuffix(prefix, "/") && !strings.HasPrefix(path, "/") {
		return prefix + "/" + path
	}
	return prefix + path
}

// Register registers handler for method and path and returns validation or
// duplicate-route errors. It is the error-returning counterpart to Handle.
func (r *Router) Register(method, path string, handler http.Handler) error {
	if handler == nil {
		return &RegistrationError{Method: method, Path: path, Err: fmt.Errorf("%w: nil handler", ErrInvalidRoute)}
	}

	middlewareSnapshot := r.middlewareSnapshot()
	return r.registerPrepared(method, path, applyMiddleware(handler, middlewareSnapshot))
}

// RegisterAlias registers aliasPath with the handler and captured middleware
// of an existing targetPath route. Optional alias middleware wraps the reused
// handler without applying the router's global middleware a second time.
//
// Alias and target path parameters must use the same names in the same order,
// and either both paths or neither path must end in a wildcard. The alias is a
// runtime-only route; integrations may choose whether to document it.
func (r *Router) RegisterAlias(method, aliasPath, targetPath string, middleware ...Middleware) error {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return &RegistrationError{Method: method, Path: aliasPath, Err: fmt.Errorf("%w: empty method", ErrInvalidRoute)}
	}

	r.mu.RLock()
	target, exists := r.routes[targetPath][method]
	targetPattern := r.patterns[targetPath]
	r.mu.RUnlock()
	if !exists {
		return fmt.Errorf("router: alias %s %s target %s: %w", method, aliasPath, targetPath, ErrRouteNotFound)
	}

	aliasPattern, err := compileRoutePattern(aliasPath)
	if err != nil {
		return &RegistrationError{Method: method, Path: aliasPath, Err: fmt.Errorf("%w: %v", ErrInvalidRoute, err)}
	}
	if !slices.Equal(aliasPattern.paramKeys, target.ParamKeys) {
		return &RegistrationError{
			Method: method,
			Path:   aliasPath,
			Err:    fmt.Errorf("%w: alias path parameters must match target %s", ErrInvalidRoute, targetPath),
		}
	}
	if (aliasPattern.kind == wildcardRoute) != (targetPattern.kind == wildcardRoute) {
		return &RegistrationError{
			Method: method,
			Path:   aliasPath,
			Err:    fmt.Errorf("%w: alias wildcard must match target %s", ErrInvalidRoute, targetPath),
		}
	}

	return r.registerPrepared(method, aliasPath, applyMiddleware(target.Handler, middleware))
}

// Handle registers handler for method and path. It panics when registration
// is invalid or duplicates an existing method/path pattern. Use Register when
// the caller needs to handle registration errors.
func (r *Router) Handle(method, path string, handler http.Handler) {
	if err := r.Register(method, path, handler); err != nil {
		panic(err)
	}
}

func (r *Router) registerPrepared(method, path string, handler http.Handler) error {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return &RegistrationError{Method: method, Path: path, Err: fmt.Errorf("%w: empty method", ErrInvalidRoute)}
	}
	if handler == nil {
		return &RegistrationError{Method: method, Path: path, Err: fmt.Errorf("%w: nil handler", ErrInvalidRoute)}
	}

	compiled, err := compileRoutePattern(path)
	if err != nil {
		return &RegistrationError{Method: method, Path: path, Err: fmt.Errorf("%w: %v", ErrInvalidRoute, err)}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.initializeLocked()

	signatureKey := method + " " + compiled.signature
	if conflictPath, exists := r.signatures[signatureKey]; exists {
		return &RegistrationError{
			Method:       method,
			Path:         path,
			ConflictPath: conflictPath,
			Err:          ErrDuplicateRoute,
		}
	}

	if _, exists := r.patterns[path]; !exists {
		r.patterns[path] = compiled
		r.orderedPaths = append(r.orderedPaths, path)
		r.sortPathsLocked()
	}
	if r.routes[path] == nil {
		r.routes[path] = make(map[string]Route)
	}
	r.routes[path][method] = Route{
		Handler:      handler,
		ParamKeys:    append([]string(nil), compiled.paramKeys...),
		ParamPattern: compiled.pattern,
	}
	r.signatures[signatureKey] = path
	return nil
}

func (r *Router) initializeLocked() {
	if r.routes == nil {
		r.routes = make(map[string]map[string]Route)
	}
	if r.patterns == nil {
		r.patterns = make(map[string]compiledRoute)
	}
	if r.signatures == nil {
		r.signatures = make(map[string]string)
	}
}

func (r *Router) middlewareSnapshot() []Middleware {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Middleware(nil), r.middleware...)
}

func (r *Router) registrations() []routeRegistration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	registrations := make([]routeRegistration, 0)
	for _, path := range r.orderedPaths {
		methods := make([]string, 0, len(r.routes[path]))
		for method := range r.routes[path] {
			methods = append(methods, method)
		}
		sort.Strings(methods)
		for _, method := range methods {
			registrations = append(registrations, routeRegistration{
				method: method,
				path:   path,
				route:  r.routes[path][method],
			})
		}
	}
	return registrations
}

// Routes returns a deterministic snapshot of registered routes.
func (r *Router) Routes() []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]RouteInfo, 0)
	for _, path := range r.orderedPaths {
		methods := make([]string, 0, len(r.routes[path]))
		for method := range r.routes[path] {
			methods = append(methods, method)
		}
		sort.Strings(methods)
		for _, method := range methods {
			result = append(result, RouteInfo{
				Method:    method,
				Path:      path,
				ParamKeys: append([]string(nil), r.routes[path][method].ParamKeys...),
				Wildcard:  r.patterns[path].kind == wildcardRoute,
			})
		}
	}
	return result
}

func (r *Router) sortPathsLocked() {
	sort.Slice(r.orderedPaths, func(i, j int) bool {
		left := r.patterns[r.orderedPaths[i]]
		right := r.patterns[r.orderedPaths[j]]
		return routePrecedes(left, right)
	})
}

func routePrecedes(left, right compiledRoute) bool {
	maxSegments := min(len(left.segmentKinds), len(right.segmentKinds))
	for i := 0; i < maxSegments; i++ {
		if left.segmentKinds[i] != right.segmentKinds[i] {
			return left.segmentKinds[i] < right.segmentKinds[i]
		}
		if left.segmentKinds[i] == parameterRoute {
			if left.segmentLiterals[i] != right.segmentLiterals[i] {
				return left.segmentLiterals[i] > right.segmentLiterals[i]
			}
			if left.segmentParams[i] != right.segmentParams[i] {
				return left.segmentParams[i] > right.segmentParams[i]
			}
		}
	}
	if len(left.segmentKinds) != len(right.segmentKinds) {
		if len(left.segmentKinds) < len(right.segmentKinds) && right.segmentKinds[len(left.segmentKinds)] == wildcardRoute {
			return true
		}
		if len(right.segmentKinds) < len(left.segmentKinds) && left.segmentKinds[len(right.segmentKinds)] == wildcardRoute {
			return false
		}
		return len(left.segmentKinds) > len(right.segmentKinds)
	}
	if left.kind == wildcardRoute && right.kind == wildcardRoute && len(left.wildcardBase) != len(right.wildcardBase) {
		return len(left.wildcardBase) > len(right.wildcardBase)
	}
	return left.path < right.path
}

func applyMiddleware(handler http.Handler, middleware []Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		if middleware[i] != nil {
			handler = middleware[i](handler)
		}
	}
	return handler
}

func compileRoutePattern(path string) (compiledRoute, error) {
	if path == "" || path[0] != '/' {
		return compiledRoute{}, errors.New("path must begin with /")
	}
	if strings.Count(path, "*") > 0 && (strings.Count(path, "*") != 1 || !strings.HasSuffix(path, "/*")) {
		return compiledRoute{}, errors.New("wildcard must be the final /* segment")
	}

	wildcard := strings.HasSuffix(path, "/*")
	matchPath := path
	wildcardBase := ""
	if wildcard {
		wildcardBase = strings.TrimSuffix(path, "/*")
		matchPath = wildcardBase
	}

	matches := routeParamPattern.FindAllStringSubmatchIndex(matchPath, -1)
	paramKeys := make([]string, 0, len(matches))
	seenParamKeys := make(map[string]bool, len(matches))
	var regexBuilder strings.Builder
	regexBuilder.WriteString("^")
	last := 0
	for _, match := range matches {
		regexBuilder.WriteString(regexp.QuoteMeta(matchPath[last:match[0]]))
		regexBuilder.WriteString("([^/]+)")
		paramKey := matchPath[match[2]:match[3]]
		if seenParamKeys[paramKey] {
			return compiledRoute{}, fmt.Errorf("duplicate path parameter %q", paramKey)
		}
		seenParamKeys[paramKey] = true
		paramKeys = append(paramKeys, paramKey)
		last = match[1]
	}
	withoutParameters := routeParamPattern.ReplaceAllString(matchPath, "")
	if strings.ContainsAny(withoutParameters, "{}") {
		return compiledRoute{}, errors.New("malformed path parameter")
	}
	regexBuilder.WriteString(regexp.QuoteMeta(matchPath[last:]))
	if wildcard {
		regexBuilder.WriteString("(?:/.*)?")
	}
	regexBuilder.WriteString("$")

	pattern, err := regexp.Compile(regexBuilder.String())
	if err != nil {
		return compiledRoute{}, err
	}

	kind := staticRoute
	if len(paramKeys) > 0 {
		kind = parameterRoute
	}
	if wildcard {
		kind = wildcardRoute
	}

	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	segmentKinds := make([]routeKind, 0, len(segments))
	segmentLiterals := make([]int, 0, len(segments))
	segmentParams := make([]int, 0, len(segments))
	for _, segment := range segments {
		parameterCount := len(routeParamPattern.FindAllStringIndex(segment, -1))
		literalLength := len(routeParamPattern.ReplaceAllString(segment, ""))
		switch {
		case segment == "*":
			segmentKinds = append(segmentKinds, wildcardRoute)
		case parameterCount > 0:
			segmentKinds = append(segmentKinds, parameterRoute)
		default:
			segmentKinds = append(segmentKinds, staticRoute)
		}
		segmentLiterals = append(segmentLiterals, literalLength)
		segmentParams = append(segmentParams, parameterCount)
	}

	signature := routeParamPattern.ReplaceAllString(path, "{}")
	return compiledRoute{
		path:            path,
		signature:       signature,
		paramKeys:       paramKeys,
		pattern:         pattern,
		kind:            kind,
		segmentKinds:    segmentKinds,
		segmentLiterals: segmentLiterals,
		segmentParams:   segmentParams,
		wildcardBase:    wildcardBase,
	}, nil
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
