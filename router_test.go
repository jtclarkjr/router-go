package router_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	router "github.com/jtclarkjr/router-go"
)

func TestHTTPMethodHelpers(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		register func(*router.Router, string, http.HandlerFunc)
	}{
		{name: "GET", method: http.MethodGet, register: (*router.Router).Get},
		{name: "POST", method: http.MethodPost, register: (*router.Router).Post},
		{name: "PUT", method: http.MethodPut, register: (*router.Router).Put},
		{name: "PATCH", method: http.MethodPatch, register: (*router.Router).Patch},
		{name: "DELETE", method: http.MethodDelete, register: (*router.Router).Delete},
		{name: "HEAD", method: http.MethodHead, register: (*router.Router).Head},
		{name: "OPTIONS", method: http.MethodOptions, register: (*router.Router).Options},
		{name: "CONNECT", method: http.MethodConnect, register: (*router.Router).Connect},
		{name: "TRACE", method: http.MethodTrace, register: (*router.Router).Trace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := router.NewRouter()
			tt.register(r, "/resource", func(w http.ResponseWriter, req *http.Request) {
				_, _ = fmt.Fprint(w, req.Method)
			})

			req := httptest.NewRequest(tt.method, "/resource", nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if body := rec.Body.String(); body != tt.method {
				t.Fatalf("body = %q, want %q", body, tt.method)
			}
		})
	}
}

func TestHandleParametersAndQuery(t *testing.T) {
	r := router.NewRouter()
	r.Handle("PURGE", "/users/{userID}/posts/{postID}", http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			_, _ = fmt.Fprintf(
				w,
				"%s/%s/%s/%s",
				router.URLParam(req, "userID"),
				router.URLParam(req, "postID"),
				router.URLParam(req, "missing"),
				router.URLQuery(req, "view"),
			)
		},
	))

	req := httptest.NewRequest("PURGE", "/users/alice/posts/42?view=full&view=compact", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got, want := rec.Body.String(), "alice/42//full"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestWildcardPrefixBehavior(t *testing.T) {
	r := router.NewRouter()
	r.Get("/assets/*", func(w http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprint(w, req.URL.Path)
	})

	for _, path := range []string{"/assets", "/assets/app.js", "/assets-legacy"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Body.String(); got != path {
				t.Fatalf("body = %q, want %q", got, path)
			}
		})
	}
}

func TestRouteGroupCopiesMiddlewareInOrder(t *testing.T) {
	var calls []string
	record := func(name string) router.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls = append(calls, name+":before")
				next.ServeHTTP(w, req)
				calls = append(calls, name+":after")
			})
		}
	}

	r := router.NewRouter()
	r.Use(record("parent"))
	r.Route("/admin", func(admin *router.Router) {
		admin.Use(record("group"))
		admin.Get("/*", func(http.ResponseWriter, *http.Request) {
			calls = append(calls, "handler")
		})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/users", nil))

	want := []string{
		"parent:before",
		"group:before",
		"handler",
		"group:after",
		"parent:after",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestPrefixedGroupRetainsInnerCompiledPattern(t *testing.T) {
	r := router.NewRouter()
	r.Route("/admin", func(admin *router.Router) {
		admin.Get("/users", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, "unexpected")
		})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/users", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want compatibility status %d", rec.Code, http.StatusNotFound)
	}
}

func TestMiddlewareAppliesAtRegistrationTime(t *testing.T) {
	var calls []string
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			calls = append(calls, req.URL.Path)
			next.ServeHTTP(w, req)
		})
	}

	r := router.NewRouter()
	r.Get("/before", func(http.ResponseWriter, *http.Request) {})
	r.Use(mw)
	r.Get("/after", func(http.ResponseWriter, *http.Request) {})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/before", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/after", nil))

	if want := []string{"/after"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("middleware calls = %v, want %v", calls, want)
	}
}

func TestDuplicateRouteUsesLastRegistration(t *testing.T) {
	r := router.NewRouter()
	r.Get("/value", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "first")
	})
	r.Get("/value", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "second")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/value", nil))

	if got := rec.Body.String(); got != "second" {
		t.Fatalf("body = %q, want second", got)
	}
}

func TestNotFoundForUnknownPathOrMethod(t *testing.T) {
	r := router.NewRouter()
	r.Get("/known", func(http.ResponseWriter, *http.Request) {})

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/unknown", nil),
		httptest.NewRequest(http.MethodPost, "/known", nil),
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want %d", req.Method, req.URL.Path, rec.Code, http.StatusNotFound)
		}
	}
}

func TestWSRoutePassesThroughNonUpgradeAndValidatesUpgrade(t *testing.T) {
	called := false
	r := router.NewRouter()
	r.WS("/ws", func(net.Conn, *http.Request) {
		called = true
	})

	plain := httptest.NewRecorder()
	r.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if plain.Code != http.StatusNotFound {
		t.Fatalf("plain request status = %d, want %d", plain.Code, http.StatusNotFound)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	upgrade := httptest.NewRecorder()
	r.ServeHTTP(upgrade, req)
	if upgrade.Code != http.StatusBadRequest {
		t.Fatalf("upgrade status = %d, want %d", upgrade.Code, http.StatusBadRequest)
	}
	if called {
		t.Fatal("WebSocket handler called for unsuccessful upgrades")
	}
}
