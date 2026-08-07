package middleware

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebSocketUpgradeDetection(t *testing.T) {
	valid := httptest.NewRequest(http.MethodGet, "/", nil)
	valid.Header.Add("Connection", "keep-alive, Upgrade")
	valid.Header.Add("Upgrade", "WebSocket")
	if !isWebSocketUpgrade(valid) {
		t.Fatal("valid request was not recognized as an upgrade")
	}
	if !headerContains(valid.Header, "Connection", "upgrade") {
		t.Fatal("headerContains did not find a case-insensitive token")
	}

	tests := []struct {
		name    string
		method  string
		conn    string
		upgrade string
	}{
		{name: "method", method: http.MethodPost, conn: "Upgrade", upgrade: "websocket"},
		{name: "connection", method: http.MethodGet, conn: "keep-alive", upgrade: "websocket"},
		{name: "upgrade", method: http.MethodGet, conn: "Upgrade", upgrade: "h2c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/", nil)
			req.Header.Set("Connection", tt.conn)
			req.Header.Set("Upgrade", tt.upgrade)
			if isWebSocketUpgrade(req) {
				t.Fatal("invalid request was recognized as an upgrade")
			}
		})
	}
}

func TestWebSocketOriginChecks(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if !checkOrigin(req, WSConfig{}) {
		t.Fatal("empty origin policy rejected request")
	}

	restricted := WSConfig{AllowedOrigins: []string{"https://app.example.com"}}
	if !checkOrigin(req, restricted) {
		t.Fatal("missing Origin was rejected")
	}

	req.Header.Set("Origin", "HTTPS://APP.EXAMPLE.COM")
	if !checkOrigin(req, restricted) {
		t.Fatal("allowed origin was rejected")
	}

	req.Header.Set("Origin", "https://blocked.example.com")
	if checkOrigin(req, restricted) {
		t.Fatal("blocked origin was accepted")
	}

	customCalls := 0
	custom := WSConfig{CheckOrigin: func(*http.Request) bool {
		customCalls++
		return false
	}}
	if checkOrigin(req, custom) {
		t.Fatal("custom origin check result was ignored")
	}
	if customCalls != 1 {
		t.Fatalf("custom origin calls = %d, want 1", customCalls)
	}
}

func TestWebSocketAcceptKeyAndHandshakeWriter(t *testing.T) {
	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	const accept = "zTH9CZQF2ErY1QDN9Mf3e5zJYF4="
	if got := computeAcceptKey(key); got != accept {
		t.Fatalf("accept key = %q, want %q", got, accept)
	}

	var output bytes.Buffer
	bufrw := bufio.NewReadWriter(
		bufio.NewReader(strings.NewReader("")),
		bufio.NewWriter(&output),
	)
	if err := writeHandshakeResponse(bufrw, accept); err != nil {
		t.Fatalf("writeHandshakeResponse returned error: %v", err)
	}

	want := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if got := output.String(); got != want {
		t.Fatalf("handshake = %q, want %q", got, want)
	}

	failing := bufio.NewReadWriter(
		bufio.NewReader(strings.NewReader("")),
		bufio.NewWriterSize(alwaysFailWriter{}, 1),
	)
	if err := writeHandshakeResponse(failing, accept); err == nil {
		t.Fatal("writeHandshakeResponse succeeded with a failing writer")
	}
}

type alwaysFailWriter struct{}

func (alwaysFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWebSocketMiddlewareFailures(t *testing.T) {
	nextCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalls++
		w.WriteHeader(http.StatusAccepted)
	})

	plain := httptest.NewRecorder()
	WebSocket(func(net.Conn, *http.Request) {})(next).ServeHTTP(
		plain,
		httptest.NewRequest(http.MethodGet, "/", nil),
	)
	if plain.Code != http.StatusAccepted || nextCalls != 1 {
		t.Fatalf("plain request: status=%d nextCalls=%d", plain.Code, nextCalls)
	}

	baseUpgrade := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		return req
	}

	tests := []struct {
		name   string
		config WSConfig
		update func(*http.Request)
		status int
		body   string
	}{
		{
			name:   "version",
			update: func(req *http.Request) { req.Header.Set("Sec-WebSocket-Version", "12") },
			status: http.StatusBadRequest,
			body:   "Unsupported WebSocket version\n",
		},
		{
			name: "key",
			update: func(req *http.Request) {
				req.Header.Set("Sec-WebSocket-Version", "13")
			},
			status: http.StatusBadRequest,
			body:   "Missing Sec-WebSocket-Key\n",
		},
		{
			name:   "origin",
			config: WSConfig{AllowedOrigins: []string{"https://allowed.example.com"}},
			update: func(req *http.Request) {
				req.Header.Set("Sec-WebSocket-Version", "13")
				req.Header.Set("Sec-WebSocket-Key", "key")
				req.Header.Set("Origin", "https://blocked.example.com")
			},
			status: http.StatusForbidden,
			body:   "Origin not allowed\n",
		},
		{
			name: "hijacker",
			update: func(req *http.Request) {
				req.Header.Set("Sec-WebSocket-Version", "13")
				req.Header.Set("Sec-WebSocket-Key", "key")
			},
			status: http.StatusInternalServerError,
			body:   "Server does not support hijacking\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := baseUpgrade()
			tt.update(req)
			rec := httptest.NewRecorder()
			WebSocketWithConfig(tt.config, func(net.Conn, *http.Request) {})(next).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := rec.Body.String(); got != tt.body {
				t.Fatalf("body = %q, want %q", got, tt.body)
			}
		})
	}

	errorWriter := hijackErrorWriter{ResponseRecorder: httptest.NewRecorder()}
	req := baseUpgrade()
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "key")
	WebSocket(func(net.Conn, *http.Request) {})(next).ServeHTTP(errorWriter, req)
	if errorWriter.Code != http.StatusInternalServerError {
		t.Fatalf("hijack error status = %d", errorWriter.Code)
	}
	if got := errorWriter.Body.String(); got != "hijack failed\n" {
		t.Fatalf("hijack error body = %q", got)
	}
}

type hijackErrorWriter struct {
	*httptest.ResponseRecorder
}

func (hijackErrorWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack failed")
}

func TestWebSocketMiddlewareHandshake(t *testing.T) {
	writer := newHijackWriter()
	defer writer.close()

	called := make(chan *http.Request, 1)
	handler := WebSocket(func(conn net.Conn, req *http.Request) {
		called <- req
		_ = conn.Close()
	})(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "keep-alive, Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(writer, req)
		close(done)
	}()

	reader := bufio.NewReader(writer.client)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if statusLine != "HTTP/1.1 101 Switching Protocols\r\n" {
		t.Fatalf("status line = %q", statusLine)
	}

	headers := make(http.Header)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
		if line == "\r\n" {
			break
		}
		name, value, ok := strings.Cut(strings.TrimSuffix(line, "\r\n"), ":")
		if !ok {
			t.Fatalf("invalid header line %q", line)
		}
		headers.Add(name, strings.TrimSpace(value))
	}
	if got := headers.Get("Sec-WebSocket-Accept"); got != "zTH9CZQF2ErY1QDN9Mf3e5zJYF4=" {
		t.Fatalf("Sec-WebSocket-Accept = %q", got)
	}

	select {
	case gotReq := <-called:
		if gotReq != req {
			t.Fatal("handler received a different request")
		}
	case <-time.After(time.Second):
		t.Fatal("WebSocket handler was not called")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WebSocket middleware did not return")
	}
}

func TestWebSocketClosesConnectionWhenHandshakeWriteFails(t *testing.T) {
	writer := newHijackWriter()
	_ = writer.client.Close()
	defer writer.close()

	called := false
	handler := WebSocket(func(net.Conn, *http.Request) {
		called = true
	})(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "key")

	handler.ServeHTTP(writer, req)
	if called {
		t.Fatal("handler called after handshake write failure")
	}
	if _, err := io.WriteString(writer.server, "closed"); err == nil {
		t.Fatal("connection remained open after handshake write failure")
	}
}
