package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// SlogLoggerConfig configures SlogLoggerWithConfig.
type SlogLoggerConfig struct {
	Logger            *slog.Logger // Logger defaults to slog.Default when nil.
	Level             slog.Level
	Message           string // Message defaults to "http request".
	RequestIDHeader   string // RequestIDHeader defaults to X-Request-ID.
	IncludeRemoteAddr bool
}

// SlogLogger writes structured request logs through slog.Default.
func SlogLogger(next http.Handler) http.Handler {
	return SlogLoggerWithConfig(SlogLoggerConfig{})(next)
}

// SlogLoggerWithConfig returns structured request logging middleware.
func SlogLoggerWithConfig(config SlogLoggerConfig) func(http.Handler) http.Handler {
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	message := strings.TrimSpace(config.Message)
	if message == "" {
		message = "http request"
	}
	requestIDHeader := strings.TrimSpace(config.RequestIDHeader)
	if requestIDHeader == "" {
		requestIDHeader = "X-Request-ID"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			wrapped := &ResponseWriterWrapper{ResponseWriter: w, StatusCode: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			attributes := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.StatusCode),
				slog.Int64("duration_ms", time.Since(started).Milliseconds()),
			}
			if requestID := strings.TrimSpace(r.Header.Get(requestIDHeader)); requestID != "" {
				attributes = append(attributes, slog.String("request_id", requestID))
			}
			if config.IncludeRemoteAddr {
				attributes = append(attributes, slog.String("remote_addr", r.RemoteAddr))
			}
			logger.LogAttrs(r.Context(), config.Level, message, attributes...)
		})
	}
}
