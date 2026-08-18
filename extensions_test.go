package router_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	router "github.com/jtclarkjr/router-go"
)

func TestRegisterAliasReusesHandlerAndCapturedMiddleware(t *testing.T) {
	r := router.NewRouter()
	globalCalls := 0
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			globalCalls++
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/v1/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprint(w, req.PathValue("id"))
	})

	aliasMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Deprecation", "true")
			next.ServeHTTP(w, req)
		})
	}
	if err := r.RegisterAlias(http.MethodGet, "/users/{id}", "/v1/users/{id}", nil, aliasMiddleware); err != nil {
		t.Fatalf("RegisterAlias: %v", err)
	}

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "42" {
		t.Fatalf("alias response = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Deprecation") != "true" {
		t.Fatal("alias middleware did not run")
	}
	if globalCalls != 1 {
		t.Fatalf("global middleware calls = %d, want 1", globalCalls)
	}
}

func TestRegisterAliasValidatesTargetAndPathShape(t *testing.T) {
	r := router.NewRouter()
	r.Get("/v1/users/{id}", func(http.ResponseWriter, *http.Request) {})

	err := r.RegisterAlias(http.MethodGet, "/users/{userID}", "/v1/users/{id}")
	if !errors.Is(err, router.ErrInvalidRoute) {
		t.Fatalf("parameter mismatch error = %v, want ErrInvalidRoute", err)
	}

	err = r.RegisterAlias(http.MethodGet, "/missing", "/v1/missing")
	if !errors.Is(err, router.ErrRouteNotFound) {
		t.Fatalf("missing target error = %v, want ErrRouteNotFound", err)
	}

	r.Get("/v1/assets/*", func(http.ResponseWriter, *http.Request) {})
	err = r.RegisterAlias(http.MethodGet, "/assets", "/v1/assets/*")
	if !errors.Is(err, router.ErrInvalidRoute) {
		t.Fatalf("wildcard mismatch error = %v, want ErrInvalidRoute", err)
	}
}
