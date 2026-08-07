package router_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	router "github.com/jtclarkjr/router-go"
)

func ExampleRouter() {
	r := router.NewRouter()
	r.Get("/hello/{name}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprintf(w, "hello %s", router.URLParam(req, "name"))
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hello/Ada", nil))
	fmt.Println(rec.Body.String())

	// Output:
	// hello Ada
}

func ExampleRouter_Route() {
	r := router.NewRouter()
	r.Route("/admin", func(admin *router.Router) {
		admin.Get("/*", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, "ok")
		})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/health", nil))
	fmt.Println(rec.Body.String())

	// Output:
	// ok
}
