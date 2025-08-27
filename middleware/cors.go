package middleware

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// CORSConfig defines the configuration for CORS middleware
type CORSConfig struct {
	// AllowedOrigins is a list of origins a cross-domain request can be executed from.
	// If the special "*" value is present in the list, all origins will be allowed.
	// An origin may contain a wildcard (*) to replace 0 or more characters
	// (i.e.: http://*.domain.com). Usage of wildcards implies a small performance penalty.
	// Only one wildcard can be used per origin.
	// Default value is ["*"]
	AllowedOrigins []string

	// AllowedMethods is a list of methods the client is allowed to use with
	// cross-domain requests. Default value is simple methods (HEAD, GET and POST).
	AllowedMethods []string

	// AllowedHeaders is list of non simple headers the client is allowed to use with
	// cross-domain requests.
	// If the special "*" value is present in the list, all headers will be allowed.
	// Default value is [] but "Origin" is always appended to the list.
	AllowedHeaders []string

	// ExposedHeaders indicates which headers are safe to expose to the API of a CORS
	// API specification
	ExposedHeaders []string

	// MaxAge indicates how long (in seconds) the results of a preflight request
	// can be cached. Default value is 0 (no cache).
	MaxAge int

	// AllowCredentials indicates whether the request can include user credentials like
	// cookies, HTTP authentication or client side SSL certificates.
	AllowCredentials bool

	// OptionsPassthrough instructs preflight to let other potential next handlers to
	// process the OPTIONS method. Turn this on if your application handles OPTIONS.
	OptionsPassthrough bool

	// Debugging turns on debug logging
	Debug bool
}

// DefaultCORSConfig returns a generic default configuration with "*" for allowed origins
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

// CORS creates a new CORS middleware with the provided configuration
func CORS(config CORSConfig) func(http.Handler) http.Handler {
	// Set defaults if not provided
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

	// Pre-compile wildcard patterns for performance
	wildcardOrigins := make([]wildcardOrigin, 0)
	allowAllOrigins := false
	for _, origin := range config.AllowedOrigins {
		if origin == "*" {
			allowAllOrigins = true
			break
		}
		if strings.Contains(origin, "*") {
			wildcardOrigins = append(wildcardOrigins, newWildcardOrigin(origin))
		}
	}

	allowAllHeaders := slices.Contains(config.AllowedHeaders, "*")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			if !isOriginAllowed(origin, config.AllowedOrigins, wildcardOrigins, allowAllOrigins) {
				if config.Debug {
					w.Header().Set("X-CORS-Debug", "Origin not allowed: "+origin)
				}
				// If origin is not allowed and this is a preflight, reject it
				if r.Method == http.MethodOptions && !config.OptionsPassthrough {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				// For non-preflight requests, continue without CORS headers
				next.ServeHTTP(w, r)
				return
			}

			// Set CORS headers
			if allowAllOrigins && !config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}

			// Set credentials header
			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Handle preflight request
			if r.Method == http.MethodOptions {
				// Set allowed methods
				if len(config.AllowedMethods) > 0 {
					w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
				}

				// Set allowed headers
				requestedHeaders := r.Header.Get("Access-Control-Request-Headers")
				if allowAllHeaders || requestedHeaders == "" {
					w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
				} else if len(config.AllowedHeaders) > 0 {
					// Check if requested headers are in the allowed list
					allowed := filterAllowedHeaders(requestedHeaders, config.AllowedHeaders)
					if allowed != "" {
						w.Header().Set("Access-Control-Allow-Headers", allowed)
					}
				}

				// Set max age
				if config.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))
				}

				if config.Debug {
					w.Header().Set("X-CORS-Debug", "Preflight response")
				}

				// If OptionsPassthrough is false, end the request here
				if !config.OptionsPassthrough {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			} else {
				// For actual requests, set exposed headers
				if len(config.ExposedHeaders) > 0 {
					w.Header().Set("Access-Control-Expose-Headers", strings.Join(config.ExposedHeaders, ", "))
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// wildcardOrigin represents a wildcard origin pattern
type wildcardOrigin struct {
	prefix string
	suffix string
}

// newWildcardOrigin creates a new wildcard origin pattern
func newWildcardOrigin(pattern string) wildcardOrigin {
	parts := strings.Split(pattern, "*")
	if len(parts) != 2 {
		// Invalid pattern, treat as exact match
		return wildcardOrigin{prefix: pattern}
	}
	return wildcardOrigin{
		prefix: parts[0],
		suffix: parts[1],
	}
}

// match checks if the origin matches the wildcard pattern
func (w wildcardOrigin) match(origin string) bool {
	if w.suffix == "" {
		return origin == w.prefix
	}
	return strings.HasPrefix(origin, w.prefix) && strings.HasSuffix(origin, w.suffix)
}

// isOriginAllowed checks if the origin is in the allowed list
func isOriginAllowed(origin string, allowedOrigins []string, wildcardOrigins []wildcardOrigin, allowAll bool) bool {
	if allowAll {
		return true
	}

	if origin == "" {
		return false
	}

	// Check exact matches
	if slices.Contains(allowedOrigins, origin) {
		return true
	}

	// Check wildcard matches
	for _, wildcard := range wildcardOrigins {
		if wildcard.match(origin) {
			return true
		}
	}

	return false
}

// filterAllowedHeaders filters the requested headers against the allowed headers
func filterAllowedHeaders(requested string, allowed []string) string {
	if requested == "" {
		return ""
	}

	// Parse requested headers
	requestedHeaders := strings.Split(requested, ",")
	for i := range requestedHeaders {
		requestedHeaders[i] = strings.TrimSpace(strings.ToLower(requestedHeaders[i]))
	}

	// Convert allowed headers to lowercase for comparison
	allowedLower := make(map[string]bool)
	for _, h := range allowed {
		allowedLower[strings.ToLower(h)] = true
	}

	// Filter requested headers
	var result []string
	for _, h := range requestedHeaders {
		if allowedLower[h] {
			result = append(result, h)
		}
	}

	return strings.Join(result, ", ")
}

// SimpleCORS creates a simple CORS middleware that allows all origins
func SimpleCORS() func(http.Handler) http.Handler {
	return CORS(DefaultCORSConfig())
}

// StrictCORS creates a strict CORS middleware with specific origins only
func StrictCORS(allowedOrigins []string) func(http.Handler) http.Handler {
	config := DefaultCORSConfig()
	config.AllowedOrigins = allowedOrigins
	config.AllowCredentials = true
	return CORS(config)
}
