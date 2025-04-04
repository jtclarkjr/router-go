package middleware

import (
	"log"
	"net/http"
	"time"
)

// ResponseWriterWrapper wraps http.ResponseWriter to capture the status code
type ResponseWriterWrapper struct {
	http.ResponseWriter
	StatusCode int
}

// WriteHeader captures the status code
func (rw *ResponseWriterWrapper) WriteHeader(code int) {
	rw.StatusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Middleware for logging requests with colorful output and response time
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now() // Start timing
		wrappedWriter := &ResponseWriterWrapper{ResponseWriter: w, StatusCode: http.StatusOK}

		// Process the request
		next.ServeHTTP(wrappedWriter, r)

		// Calculate response time
		duration := time.Since(start)
		durationColor := getDurationColor(duration)

		// Determine the color based on the status code
		statusColor := getStatusColor(wrappedWriter.StatusCode)
		methodColor := getMethodColor(r.Method)
		resetColor := "\033[0m"

		// Log the request with colors and response time
		log.Printf("%s%s%s %s%s%s from %s - %s%d%s in %s%s%s",
			methodColor, r.Method, resetColor,
			statusColor, r.URL.Path, resetColor,
			r.RemoteAddr,
			statusColor, wrappedWriter.StatusCode, resetColor,
			durationColor, duration, resetColor,
		)
	})
}

// getStatusColor returns the color for a given status code
func getStatusColor(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return "\033[32m" // Green for success
	case statusCode >= 300 && statusCode < 400:
		return "\033[36m" // Cyan for redirects
	case statusCode >= 400 && statusCode < 500:
		return "\033[33m" // Yellow for client errors
	case statusCode >= 500:
		return "\033[31m" // Red for server errors
	default:
		return "\033[0m" // Default color
	}
}

// getMethodColor returns the color for a given HTTP method
func getMethodColor(method string) string {
	switch method {
	case http.MethodGet:
		return "\033[34m" // Blue for GET
	case http.MethodPost:
		return "\033[36m" // Cyan for POST
	case http.MethodPut:
		return "\033[33m" // Yellow for PUT
	case http.MethodDelete:
		return "\033[31m" // Red for DELETE
	default:
		return "\033[0m" // Default color
	}
}

// getDurationColor returns the color for a given response time
func getDurationColor(duration time.Duration) string {
	switch {
	case duration < 100*time.Millisecond:
		return "\033[32m" // Green for fast responses (< 100ms)
	case duration < 500*time.Millisecond:
		return "\033[33m" // Yellow for moderate responses (100ms - 500ms)
	default:
		return "\033[31m" // Red for slow responses (> 500ms)
	}
}
