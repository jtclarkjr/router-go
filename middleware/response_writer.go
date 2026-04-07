package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
)

// ResponseWriterWrapper wraps http.ResponseWriter to capture the status code
// while preserving interfaces like http.Hijacker for WebSocket upgrades.
type ResponseWriterWrapper struct {
	http.ResponseWriter
	StatusCode int
}

// WriteHeader captures the status code.
func (rw *ResponseWriterWrapper) WriteHeader(code int) {
	rw.StatusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker by delegating to the underlying ResponseWriter.
func (rw *ResponseWriterWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}
