package router_test

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
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

	for _, path := range []string{"/assets", "/assets/app.js"} {
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

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets-legacy", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("boundary-unsafe wildcard status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestExactRoutePrecedesWildcardAtItsBase(t *testing.T) {
	r := router.NewRouter()
	r.Get("/assets/*", textHandler("wildcard"))
	r.Get("/assets", textHandler("exact"))
	r.Get("/files/{id}/*", textHandler("parameter wildcard"))
	r.Get("/files/{id}", textHandler("parameter exact"))

	for path, want := range map[string]string{
		"/assets":          "exact",
		"/assets/app.js":   "wildcard",
		"/files/report":    "parameter exact",
		"/files/report/v1": "parameter wildcard",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Body.String(); got != want {
			t.Fatalf("path %s body = %q, want %q", path, got, want)
		}
	}
}

func TestLiteralConstrainedParameterSegmentPrecedesGenericParameter(t *testing.T) {
	r := router.NewRouter()
	r.Get("/files/{id}", textHandler("generic"))
	r.Get("/files/{name}.json", textHandler("json"))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/report.json", nil))
	if got := rec.Body.String(); got != "json" {
		t.Fatalf("body = %q, want json", got)
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

func TestPrefixedGroupCompilesFullPath(t *testing.T) {
	r := router.NewRouter()
	r.Route("/admin", func(admin *router.Router) {
		admin.Get("/users", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, "grouped")
		})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/users", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "grouped" {
		t.Fatalf("body = %q, want grouped", got)
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

func TestDuplicateRouteIsRejected(t *testing.T) {
	r := router.NewRouter()
	if err := r.Register(http.MethodGet, "/value/{id}", http.NotFoundHandler()); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	err := r.Register(http.MethodGet, "/value/{name}", http.NotFoundHandler())
	if !errors.Is(err, router.ErrDuplicateRoute) {
		t.Fatalf("duplicate error = %v, want ErrDuplicateRoute", err)
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("Handle did not panic for a duplicate route")
		}
	}()
	r.Get("/value/{another}", func(http.ResponseWriter, *http.Request) {})
	if err == nil {
		t.Fatal("unreachable")
	}
}

func TestEquivalentPatternsCanUseMethodSpecificParameterNames(t *testing.T) {
	r := router.NewRouter()
	r.Get("/value/{id}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprint(w, "get:"+req.PathValue("id"))
	})
	if err := r.Register(http.MethodPost, "/value/{name}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprint(w, "post:"+req.PathValue("name"))
	})); err != nil {
		t.Fatalf("register method-specific path name: %v", err)
	}

	for method, want := range map[string]string{
		http.MethodGet:  "get:item",
		http.MethodPost: "post:item",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, "/value/item", nil))
		if got := rec.Body.String(); got != want {
			t.Fatalf("%s body = %q, want %q", method, got, want)
		}
	}

	wrongMethod := httptest.NewRecorder()
	r.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodPut, "/value/item", nil))
	if got := wrongMethod.Header().Get("Allow"); got != "GET, POST" {
		t.Fatalf("Allow = %q, want GET, POST", got)
	}
}

func TestInvalidRoutesAreRejected(t *testing.T) {
	tests := []struct {
		method string
		path   string
	}{
		{method: "", path: "/valid"},
		{method: http.MethodGet, path: "missing-slash"},
		{method: http.MethodGet, path: "/wild/*/tail"},
		{method: http.MethodGet, path: "/wild/*/tail/*"},
		{method: http.MethodGet, path: "/malformed/{id"},
		{method: http.MethodGet, path: "/duplicate/{id}/{id}"},
	}
	for _, test := range tests {
		err := router.NewRouter().Register(test.method, test.path, http.NotFoundHandler())
		if !errors.Is(err, router.ErrInvalidRoute) {
			t.Errorf("Register(%q, %q) error = %v, want ErrInvalidRoute", test.method, test.path, err)
		}
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	r := router.NewRouter()
	r.Get("/known", func(http.ResponseWriter, *http.Request) {})
	r.Post("/known", func(http.ResponseWriter, *http.Request) {})

	unknown := httptest.NewRecorder()
	r.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/unknown", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown status = %d, want %d", unknown.Code, http.StatusNotFound)
	}

	wrongMethod := httptest.NewRecorder()
	r.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodPut, "/known", nil))
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status = %d, want %d", wrongMethod.Code, http.StatusMethodNotAllowed)
	}
	if got := wrongMethod.Header().Get("Allow"); got != "GET, POST" {
		t.Fatalf("Allow = %q, want GET, POST", got)
	}
}

func TestHeadAndOptionsRequireExplicitRegistration(t *testing.T) {
	r := router.NewRouter()
	r.Get("/known", func(http.ResponseWriter, *http.Request) {})

	for _, method := range []string{http.MethodHead, http.MethodOptions} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, "/known", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want 405", method, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodGet {
			t.Fatalf("%s Allow = %q, want GET", method, got)
		}
	}
}

func TestDeterministicRoutePrecedence(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		r := router.NewRouter()
		r.Get("/groups/*", textHandler("wildcard"))
		r.Get("/groups/{groupID}", textHandler("parameter"))
		r.Get("/groups/users", textHandler("static"))

		for path, want := range map[string]string{
			"/groups/users":       "static",
			"/groups/123":         "parameter",
			"/groups/123/members": "wildcard",
		} {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if got := rec.Body.String(); got != want {
				t.Fatalf("iteration %d path %s body = %q, want %q", iteration, path, got, want)
			}
		}
	}
}

func TestPathValuesAvailableThroughStdlibAndCompatibilityHelper(t *testing.T) {
	r := router.NewRouter()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprintf(w, "%s/%s", req.PathValue("id"), router.URLParam(req, "id"))
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/alice", nil))
	if got := rec.Body.String(); got != "alice/alice" {
		t.Fatalf("body = %q, want alice/alice", got)
	}
}

func TestPathValuesDoNotMutateOuterRequest(t *testing.T) {
	r := router.NewRouter()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprint(w, req.PathValue("id"))
	})
	req := httptest.NewRequest(http.MethodGet, "/users/inner", nil)
	req.SetPathValue("id", "outer")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if got := rec.Body.String(); got != "inner" {
		t.Fatalf("handler path value = %q, want inner", got)
	}
	if got := req.PathValue("id"); got != "outer" {
		t.Fatalf("outer request path value = %q, want outer", got)
	}
}

func TestRoutesReturnsDeterministicSnapshot(t *testing.T) {
	r := router.NewRouter()
	r.Post("/items/{id}", func(http.ResponseWriter, *http.Request) {})
	r.Get("/items/static", func(http.ResponseWriter, *http.Request) {})
	r.Get("/items/{id}", func(http.ResponseWriter, *http.Request) {})

	got := r.Routes()
	want := []router.RouteInfo{
		{Method: http.MethodGet, Path: "/items/static"},
		{Method: http.MethodGet, Path: "/items/{id}", ParamKeys: []string{"id"}},
		{Method: http.MethodPost, Path: "/items/{id}", ParamKeys: []string{"id"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %#v, want %#v", got, want)
	}
}

func TestConcurrentRegistrationAndDispatch(t *testing.T) {
	r := router.NewRouter()
	r.Get("/stable", func(http.ResponseWriter, *http.Request) {})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := r.Register(http.MethodGet, fmt.Sprintf("/dynamic/%d", i), http.NotFoundHandler()); err != nil {
				t.Errorf("register route %d: %v", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stable", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("stable status = %d, want %d", rec.Code, http.StatusOK)
			}
		}()
	}
	wg.Wait()
}

func TestZeroValueRouterAndMethodNormalization(t *testing.T) {
	var r router.Router
	if err := r.Register("get", "/zero", textHandler("ready")); err != nil {
		t.Fatalf("zero-value Register: %v", err)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/zero", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ready" {
		t.Fatalf("zero-value response = %d %q", rec.Code, rec.Body.String())
	}
}

func textHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body)
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
