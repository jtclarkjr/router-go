package middleware

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
)

// WSHandler is a callback that receives the hijacked connection after a successful WebSocket upgrade.
type WSHandler func(conn net.Conn, r *http.Request)

// WSConfig defines configuration options for the WebSocket upgrade middleware.
type WSConfig struct {
	// AllowedOrigins is a list of allowed origins. An empty list allows all origins.
	AllowedOrigins []string

	// CheckOrigin is a custom function to validate the request origin.
	// If set, AllowedOrigins is ignored.
	CheckOrigin func(r *http.Request) bool
}

// wsGUID is the magic GUID defined in RFC 6455 Section 4.2.2.
const wsGUID = "258EAFA5-E914-47DA-95CA-5AB5DC85B7E2"

// WebSocket returns middleware that upgrades qualifying requests to WebSocket
// connections using the RFC 6455 handshake. Non-WebSocket requests pass through
// to the next handler.
func WebSocket(handler WSHandler) func(http.Handler) http.Handler {
	return WebSocketWithConfig(WSConfig{}, handler)
}

// WebSocketWithConfig returns middleware that upgrades qualifying requests to
// WebSocket connections with the provided configuration.
func WebSocketWithConfig(config WSConfig, handler WSHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only consider GET requests with the upgrade headers.
			if !isWebSocketUpgrade(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Validate Sec-WebSocket-Version.
			if r.Header.Get("Sec-WebSocket-Version") != "13" {
				http.Error(w, "Unsupported WebSocket version", http.StatusBadRequest)
				return
			}

			// Validate Sec-WebSocket-Key.
			key := r.Header.Get("Sec-WebSocket-Key")
			if key == "" {
				http.Error(w, "Missing Sec-WebSocket-Key", http.StatusBadRequest)
				return
			}

			// Origin check.
			if !checkOrigin(r, config) {
				http.Error(w, "Origin not allowed", http.StatusForbidden)
				return
			}

			// Hijack the connection.
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "Server does not support hijacking", http.StatusInternalServerError)
				return
			}

			conn, bufrw, err := hj.Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Compute accept key per RFC 6455.
			acceptKey := computeAcceptKey(key)

			// Write the 101 Switching Protocols response.
			if err := writeHandshakeResponse(bufrw, acceptKey); err != nil {
				_ = conn.Close()
				return
			}

			handler(conn, r)
		})
	}
}

// isWebSocketUpgrade checks whether the request is a WebSocket upgrade request.
func isWebSocketUpgrade(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if !headerContains(r.Header, "Connection", "upgrade") {
		return false
	}
	if !headerContains(r.Header, "Upgrade", "websocket") {
		return false
	}
	return true
}

// headerContains returns true if the header value contains the target token
// (case-insensitive, comma-separated).
func headerContains(h http.Header, key, target string) bool {
	for _, v := range h[http.CanonicalHeaderKey(key)] {
		for _, s := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(s), target) {
				return true
			}
		}
	}
	return false
}

// checkOrigin validates the request origin against the config.
func checkOrigin(r *http.Request, config WSConfig) bool {
	if config.CheckOrigin != nil {
		return config.CheckOrigin(r)
	}

	// No restrictions if AllowedOrigins is empty.
	if len(config.AllowedOrigins) == 0 {
		return true
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser clients may omit Origin; allow by default.
		return true
	}

	for _, allowed := range config.AllowedOrigins {
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}
	return false
}

// computeAcceptKey computes the Sec-WebSocket-Accept value per RFC 6455.
func computeAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// writeHandshakeResponse writes the HTTP 101 response to complete the upgrade.
func writeHandshakeResponse(bufrw *bufio.ReadWriter, acceptKey string) error {
	if _, err := bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\n"); err != nil {
		return err
	}
	if _, err := bufrw.WriteString("Upgrade: websocket\r\n"); err != nil {
		return err
	}
	if _, err := bufrw.WriteString("Connection: Upgrade\r\n"); err != nil {
		return err
	}
	if _, err := bufrw.WriteString("Sec-WebSocket-Accept: " + acceptKey + "\r\n"); err != nil {
		return err
	}
	if _, err := bufrw.WriteString("\r\n"); err != nil {
		return err
	}
	return bufrw.Flush()
}
