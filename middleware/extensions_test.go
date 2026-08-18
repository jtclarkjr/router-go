package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireEnvVars(t *testing.T) {
	const present = "ROUTER_GO_STARTUP_PRESENT"
	const missing = "ROUTER_GO_STARTUP_MISSING"
	t.Setenv(present, "yes")
	t.Setenv(missing, "")

	if err := RequireEnvVars(present); err != nil {
		t.Fatalf("present environment: %v", err)
	}
	if got := MissingEnvVars(present, missing); len(got) != 1 || got[0] != missing {
		t.Fatalf("MissingEnvVars = %v, want [%s]", got, missing)
	}
	err := RequireEnvVars(present, missing)
	var missingErr *MissingEnvVarsError
	if !errors.As(err, &missingErr) {
		t.Fatalf("RequireEnvVars error = %v, want MissingEnvVarsError", err)
	}
	if got := missingErr.Error(); got != "Missing required environment variables: ["+missing+"]" {
		t.Fatalf("error = %q", got)
	}
}

func TestSlogLoggerWithConfig(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := SlogLoggerWithConfig(SlogLoggerConfig{
		Logger:            logger,
		Message:           "request complete",
		IncludeRemoteAddr: true,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	request := httptest.NewRequest(http.MethodPost, "/items", nil)
	request.Header.Set("X-Request-ID", "request-1")
	request.RemoteAddr = "192.0.2.1:1234"
	handler.ServeHTTP(httptest.NewRecorder(), request)

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &entry); err != nil {
		t.Fatalf("decode slog output: %v", err)
	}
	for key, want := range map[string]any{
		"msg":         "request complete",
		"method":      http.MethodPost,
		"path":        "/items",
		"status":      float64(http.StatusCreated),
		"request_id":  "request-1",
		"remote_addr": "192.0.2.1:1234",
	} {
		if got := entry[key]; got != want {
			t.Errorf("%s = %#v, want %#v", key, got, want)
		}
	}
	if duration, ok := entry["duration_ms"].(float64); !ok || duration < 0 {
		t.Fatalf("duration_ms = %#v", entry["duration_ms"])
	}
}

func TestRecovererWithHandler(t *testing.T) {
	called := false
	handler := RecovererWithHandler(func(w http.ResponseWriter, req *http.Request, recovered any, stack []byte) {
		called = true
		if recovered != "boom" {
			t.Errorf("recovered = %#v", recovered)
		}
		if req.URL.Path != "/panic" || !strings.Contains(string(stack), "TestRecovererWithHandler") {
			t.Errorf("unexpected recovery context: path=%s stack=%q", req.URL.Path, stack)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"custom"}`))
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if !called || recorder.Code != http.StatusInternalServerError || recorder.Body.String() != `{"error":"custom"}` {
		t.Fatalf("recovery = called:%v status:%d body:%q", called, recorder.Code, recorder.Body.String())
	}

	if RecovererWithHandler(nil) == nil {
		t.Fatal("nil panic handler did not return fallback middleware")
	}
}
