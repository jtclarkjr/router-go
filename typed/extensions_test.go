package typed_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	router "github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/openapi"
	"github.com/jtclarkjr/router-go/typed"
)

type middlewareContextKey struct{}

func TestOperationMiddlewareRunsBeforeTypedBinding(t *testing.T) {
	r := typed.New(router.NewRouter())
	blockedHandlerCalled := false
	block := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
	type input struct {
		Name string `json:"name" validate:"required"`
	}
	if err := typed.RegisterWithMiddleware(r, typed.Operation[input, typed.NoBody]{
		Method: http.MethodPost,
		Path:   "/typed",
	}, func(*http.Request, input) (typed.Response[typed.NoBody], error) {
		blockedHandlerCalled = true
		return typed.Response[typed.NoBody]{}, nil
	}, nil, block); err != nil {
		t.Fatalf("Register: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/typed", strings.NewReader("not-json"))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if blockedHandlerCalled {
		t.Fatal("typed handler ran after middleware rejected the request")
	}
}

func TestOperationMiddlewareCanEnrichTypedRequestContext(t *testing.T) {
	r := typed.New(router.NewRouter())
	enrich := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), middlewareContextKey{}, "user-1")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
	if err := typed.RegisterWithMiddleware(r, typed.Operation[typed.Empty, typed.NoBody]{
		Method: http.MethodGet,
		Path:   "/context",
	}, func(req *http.Request, _ typed.Empty) (typed.Response[typed.NoBody], error) {
		if req.Context().Value(middlewareContextKey{}) != "user-1" {
			t.Fatal("middleware context value was not visible to typed handler")
		}
		return typed.Response[typed.NoBody]{}, nil
	}, enrich); err != nil {
		t.Fatalf("Register: %v", err)
	}

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/context", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestRawOperationMiddleware(t *testing.T) {
	r := typed.New(router.NewRouter())
	middlewareCalled := false
	mark := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			middlewareCalled = true
			next.ServeHTTP(w, req)
		})
	}
	err := typed.RegisterRawWithMiddleware(r, typed.RawOperation{
		Method: http.MethodGet,
		Path:   "/raw",
		Kind:   typed.RawHTTP,
		Spec: openapi.Operation{Responses: map[string]openapi.Response{
			"204": {Description: "No content"},
		}},
	}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), mark)
	if err != nil {
		t.Fatalf("RegisterRaw: %v", err)
	}

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/raw", nil))
	if recorder.Code != http.StatusNoContent || !middlewareCalled {
		t.Fatalf("raw response = %d, middleware called = %v", recorder.Code, middlewareCalled)
	}
}
