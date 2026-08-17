package router_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	router "github.com/jtclarkjr/router-go"
)

func FuzzRouterRegistrationAndDispatch(f *testing.F) {
	for _, seed := range []string{
		"/",
		"/users/{id}",
		"/assets/*",
		"/files/{name}.json",
		"/malformed/{id",
		"relative",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, path string) {
		r := router.NewRouter()
		if err := r.Register(http.MethodGet, path, http.NotFoundHandler()); err != nil {
			return
		}
		req := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}, Header: make(http.Header)}
		r.ServeHTTP(httptest.NewRecorder(), req)
	})
}
