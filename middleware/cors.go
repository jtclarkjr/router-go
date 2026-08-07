package middleware

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// CORSConfig configures the CORS middleware.
type CORSConfig struct {
	// AllowedOrigins lists origins that may make cross-origin requests.
	// The value "*" allows every origin. One wildcard may appear within an
	// origin, for example "https://*.example.com".
	AllowedOrigins []string

	// AllowedMethods lists methods returned for preflight requests.
	AllowedMethods []string

	// AllowedHeaders lists request headers accepted during preflight.
	// The value "*" reflects every requested header.
	AllowedHeaders []string

	// ExposedHeaders lists response headers exposed to browser clients.
	ExposedHeaders []string

	// MaxAge is the number of seconds a browser may cache a preflight response.
	MaxAge int

	// AllowCredentials permits credentials on cross-origin requests.
	AllowCredentials bool

	// OptionsPassthrough passes preflight requests to the next handler.
	OptionsPassthrough bool

	// Debug adds an X-CORS-Debug response header.
	Debug bool
}

// DefaultCORSConfig returns the permissive default CORS configuration.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodHead,
			http.MethodOptions,
		},
		AllowedHeaders:     []string{"*"},
		AllowCredentials:   false,
		OptionsPassthrough: false,
		Debug:              false,
	}
}

type corsHandler struct {
	config          CORSConfig
	next            http.Handler
	wildcardOrigins []wildcardOrigin
	allowAllOrigins bool
	allowAllHeaders bool
}

// CORS returns middleware configured by config.
func CORS(config CORSConfig) func(http.Handler) http.Handler {
	config = applyCORSDefaults(config)
	wildcardOrigins, allowAllOrigins := compileAllowedOrigins(config.AllowedOrigins)
	allowAllHeaders := slices.Contains(config.AllowedHeaders, "*")

	return func(next http.Handler) http.Handler {
		handler := &corsHandler{
			config:          config,
			next:            next,
			wildcardOrigins: wildcardOrigins,
			allowAllOrigins: allowAllOrigins,
			allowAllHeaders: allowAllHeaders,
		}
		return http.HandlerFunc(handler.serveHTTP)
	}
}

func applyCORSDefaults(config CORSConfig) CORSConfig {
	if len(config.AllowedOrigins) == 0 {
		config.AllowedOrigins = []string{"*"}
	}
	if len(config.AllowedMethods) == 0 {
		config.AllowedMethods = []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodHead,
		}
	}
	return config
}

func compileAllowedOrigins(allowedOrigins []string) ([]wildcardOrigin, bool) {
	wildcardOrigins := make([]wildcardOrigin, 0)
	for _, origin := range allowedOrigins {
		if origin == "*" {
			return wildcardOrigins, true
		}
		if strings.Contains(origin, "*") {
			wildcardOrigins = append(wildcardOrigins, newWildcardOrigin(origin))
		}
	}
	return wildcardOrigins, false
}

func (h *corsHandler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if !isOriginAllowed(origin, h.config.AllowedOrigins, h.wildcardOrigins, h.allowAllOrigins) {
		h.serveDisallowedOrigin(w, r, origin)
		return
	}

	h.setOriginHeaders(w, origin)

	if r.Method == http.MethodOptions {
		if h.servePreflight(w, r) {
			return
		}
	} else {
		h.setExposedHeaders(w)
	}

	h.next.ServeHTTP(w, r)
}

func (h *corsHandler) serveDisallowedOrigin(w http.ResponseWriter, r *http.Request, origin string) {
	if h.config.Debug {
		w.Header().Set("X-CORS-Debug", "Origin not allowed: "+origin)
	}
	if r.Method == http.MethodOptions && !h.config.OptionsPassthrough {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	h.next.ServeHTTP(w, r)
}

func (h *corsHandler) setOriginHeaders(w http.ResponseWriter, origin string) {
	if h.allowAllOrigins && !h.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
	}

	if h.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

func (h *corsHandler) servePreflight(w http.ResponseWriter, r *http.Request) bool {
	if len(h.config.AllowedMethods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(h.config.AllowedMethods, ", "))
	}

	h.setAllowedHeaders(w, r.Header.Get("Access-Control-Request-Headers"))

	if h.config.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", strconv.Itoa(h.config.MaxAge))
	}
	if h.config.Debug {
		w.Header().Set("X-CORS-Debug", "Preflight response")
	}
	if h.config.OptionsPassthrough {
		return false
	}

	w.WriteHeader(http.StatusNoContent)
	return true
}

func (h *corsHandler) setAllowedHeaders(w http.ResponseWriter, requested string) {
	if h.allowAllHeaders || requested == "" {
		w.Header().Set("Access-Control-Allow-Headers", requested)
		return
	}
	if len(h.config.AllowedHeaders) == 0 {
		return
	}

	if allowed := filterAllowedHeaders(requested, h.config.AllowedHeaders); allowed != "" {
		w.Header().Set("Access-Control-Allow-Headers", allowed)
	}
}

func (h *corsHandler) setExposedHeaders(w http.ResponseWriter) {
	if len(h.config.ExposedHeaders) > 0 {
		w.Header().Set("Access-Control-Expose-Headers", strings.Join(h.config.ExposedHeaders, ", "))
	}
}

type wildcardOrigin struct {
	prefix string
	suffix string
}

func newWildcardOrigin(pattern string) wildcardOrigin {
	parts := strings.Split(pattern, "*")
	if len(parts) != 2 {
		return wildcardOrigin{prefix: pattern}
	}
	return wildcardOrigin{
		prefix: parts[0],
		suffix: parts[1],
	}
}

func (w wildcardOrigin) match(origin string) bool {
	if w.suffix == "" {
		return origin == w.prefix
	}
	return strings.HasPrefix(origin, w.prefix) && strings.HasSuffix(origin, w.suffix)
}

func isOriginAllowed(origin string, allowedOrigins []string, wildcardOrigins []wildcardOrigin, allowAll bool) bool {
	if allowAll {
		return true
	}
	if origin == "" {
		return false
	}
	if slices.Contains(allowedOrigins, origin) {
		return true
	}

	for _, wildcard := range wildcardOrigins {
		if wildcard.match(origin) {
			return true
		}
	}
	return false
}

func filterAllowedHeaders(requested string, allowed []string) string {
	if requested == "" {
		return ""
	}

	requestedHeaders := strings.Split(requested, ",")
	for i := range requestedHeaders {
		requestedHeaders[i] = strings.TrimSpace(strings.ToLower(requestedHeaders[i]))
	}

	allowedLower := make(map[string]bool)
	for _, header := range allowed {
		allowedLower[strings.ToLower(header)] = true
	}

	result := make([]string, 0, len(requestedHeaders))
	for _, header := range requestedHeaders {
		if allowedLower[header] {
			result = append(result, header)
		}
	}
	return strings.Join(result, ", ")
}

// SimpleCORS returns middleware that allows every origin.
func SimpleCORS() func(http.Handler) http.Handler {
	return CORS(DefaultCORSConfig())
}

// StrictCORS returns credentialed CORS middleware for allowedOrigins.
func StrictCORS(allowedOrigins []string) func(http.Handler) http.Handler {
	config := DefaultCORSConfig()
	config.AllowedOrigins = allowedOrigins
	config.AllowCredentials = true
	return CORS(config)
}
