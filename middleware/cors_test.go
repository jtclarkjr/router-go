package middleware

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestDefaultCORSConfig(t *testing.T) {
	config := DefaultCORSConfig()

	if !slices.Equal(config.AllowedOrigins, []string{"*"}) {
		t.Fatalf("AllowedOrigins = %v", config.AllowedOrigins)
	}
	wantMethods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodHead,
		http.MethodOptions,
	}
	if !slices.Equal(config.AllowedMethods, wantMethods) {
		t.Fatalf("AllowedMethods = %v, want %v", config.AllowedMethods, wantMethods)
	}
	if !slices.Equal(config.AllowedHeaders, []string{"*"}) {
		t.Fatalf("AllowedHeaders = %v", config.AllowedHeaders)
	}
	if config.AllowCredentials || config.OptionsPassthrough || config.Debug || config.MaxAge != 0 {
		t.Fatalf("unexpected non-zero defaults: %+v", config)
	}
}

func TestSimpleCORSActualRequest(t *testing.T) {
	nextCalled := false
	handler := SimpleCORS()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusCreated)
	}))
	if _, ok := handler.(http.HandlerFunc); !ok {
		t.Fatalf("middleware handler type = %T, want http.HandlerFunc", handler)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if !nextCalled {
		t.Fatal("next handler was not called")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
}

func TestCORSAllowedCredentialedRequest(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins:   []string{"https://app.example.com"},
		ExposedHeaders:   []string{"X-Total", "X-Page"},
		AllowCredentials: true,
	}
	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	assertHeader(t, rec, "Access-Control-Allow-Origin", "https://app.example.com")
	assertHeader(t, rec, "Access-Control-Allow-Credentials", "true")
	assertHeader(t, rec, "Access-Control-Expose-Headers", "X-Total, X-Page")
	assertHeader(t, rec, "Vary", "Origin")
}

func TestCORSDisallowedRequests(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"https://allowed.example.com"},
		Debug:          true,
	}
	nextCalls := 0
	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalls++
		w.WriteHeader(http.StatusAccepted)
	}))

	actualReq := httptest.NewRequest(http.MethodGet, "/", nil)
	actualReq.Header.Set("Origin", "https://blocked.example.com")
	actual := httptest.NewRecorder()
	handler.ServeHTTP(actual, actualReq)

	if actual.Code != http.StatusAccepted {
		t.Fatalf("actual status = %d, want %d", actual.Code, http.StatusAccepted)
	}
	assertHeader(t, actual, "X-CORS-Debug", "Origin not allowed: https://blocked.example.com")
	if got := actual.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allow-origin header %q", got)
	}

	preflightReq := httptest.NewRequest(http.MethodOptions, "/", nil)
	preflightReq.Header.Set("Origin", "https://blocked.example.com")
	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, preflightReq)

	if preflight.Code != http.StatusForbidden {
		t.Fatalf("preflight status = %d, want %d", preflight.Code, http.StatusForbidden)
	}
	if nextCalls != 1 {
		t.Fatalf("next calls = %d, want 1", nextCalls)
	}
}

func TestCORSPreflight(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		AllowedMethods: []string{http.MethodGet, http.MethodPost},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		MaxAge:         600,
		Debug:          true,
	}
	nextCalled := false
	handler := CORS(config)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Other, AUTHORIZATION")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if nextCalled {
		t.Fatal("next handler called for terminating preflight")
	}
	assertHeader(t, rec, "Access-Control-Allow-Methods", "GET, POST")
	assertHeader(t, rec, "Access-Control-Allow-Headers", "content-type, authorization")
	assertHeader(t, rec, "Access-Control-Max-Age", "600")
	assertHeader(t, rec, "X-CORS-Debug", "Preflight response")
}

func TestCORSPreflightPassthroughAndAllHeaders(t *testing.T) {
	config := CORSConfig{
		AllowedOrigins:     []string{"*"},
		AllowedHeaders:     []string{"*"},
		OptionsPassthrough: true,
	}
	nextCalled := false
	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://any.example.com")
	req.Header.Set("Access-Control-Request-Headers", "X-Custom, Authorization")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if !nextCalled {
		t.Fatal("next handler was not called")
	}
	assertHeader(t, rec, "Access-Control-Allow-Headers", "X-Custom, Authorization")
}

func TestCORSWildcardAndStrictHelpers(t *testing.T) {
	wildcard := newWildcardOrigin("https://*.example.com")
	if !wildcard.match("https://api.example.com") {
		t.Fatal("wildcard origin did not match")
	}
	if wildcard.match("https://api.example.net") {
		t.Fatal("wildcard origin matched an unexpected suffix")
	}

	trailing := newWildcardOrigin("https://example.com*")
	if !trailing.match("https://example.com") {
		t.Fatal("trailing wildcard did not preserve exact-prefix behavior")
	}
	if trailing.match("https://example.com/path") {
		t.Fatal("trailing wildcard unexpectedly matched an extended origin")
	}

	invalid := newWildcardOrigin("https://*.*.example.com")
	if invalid.match("https://a.b.example.com") {
		t.Fatal("multi-wildcard pattern unexpectedly matched")
	}

	handler := StrictCORS([]string{"https://app.example.com"})(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertHeader(t, rec, "Access-Control-Allow-Credentials", "true")
	assertHeader(t, rec, "Access-Control-Allow-Origin", "https://app.example.com")
}

func TestFilterAllowedHeaders(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		allowed   []string
		want      string
	}{
		{name: "empty", requested: "", allowed: []string{"X-One"}, want: ""},
		{name: "filtered", requested: "X-One, X-Two", allowed: []string{"x-two"}, want: "x-two"},
		{name: "none", requested: "X-One", allowed: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filterAllowedHeaders(tt.requested, tt.allowed); got != tt.want {
				t.Fatalf("filterAllowedHeaders() = %q, want %q", got, tt.want)
			}
		})
	}
}

func assertHeader(t *testing.T, rec *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := rec.Header().Get(name); got != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}
