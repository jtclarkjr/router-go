# router-go

A small, standard-library HTTP router with route groups, path parameters,
middleware, CORS helpers, request logging, rate limiting, and WebSocket
upgrades.

The module intentionally keeps its API close to `net/http`: `*router.Router`
implements `http.Handler`, while middleware uses
`func(http.Handler) http.Handler`.

## Installation

```bash
go get github.com/jtclarkjr/router-go
```

## Quick start

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	router "github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/middleware"
)

func main() {
	r := router.NewRouter()

	// Middleware applies to routes registered after each Use call.
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Throttle(5))

	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := router.URLParam(req, "id")
		fmt.Fprintf(w, "user %s", id)
	})

	log.Fatal(http.ListenAndServe(":8080", r))
}
```

## Routing

The router includes helpers for GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS,
CONNECT, and TRACE. Use `Handle` for another method or an `http.Handler`
instead of an `http.HandlerFunc`.

```go
r.Get("/users", listUsers)
r.Post("/users", createUser)
r.Handle("PURGE", "/cache", purgeHandler)
```

A path segment written as `{name}` is available through `URLParam`.
`URLQuery` is a convenience wrapper around `req.URL.Query().Get`.

```go
r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
	id := router.URLParam(req, "id")
	filter := router.URLQuery(req, "filter")
	fmt.Fprintf(w, "%s:%s", id, filter)
})
```

Paths ending in `/*` use prefix matching.

```go
r.Get("/assets/*", assetHandler)
```

### Route groups

`Route` creates a temporary subrouter and copies the middleware already
installed on its parent. For compatibility with the current matcher, prefixed
groups dispatch through an inner wildcard route.

```go
r.Route("/admin", func(admin *router.Router) {
	admin.Use(requireAdmin)
	admin.Get("/*", adminHandler)
})
```

Middleware is wrapped at registration time. Calling `Use` does not alter
routes that were registered earlier.

## Middleware

The `middleware` package contains independent `net/http` middleware and
supporting utilities:

- `Logger` and `LoggerWithConfig`
- `Recoverer`
- `CORS`, `SimpleCORS`, and `StrictCORS`
- `RateLimiter`
- `Throttle`
- `EnvVarChecker`
- `WebSocket` and `WebSocketWithConfig`
- `APIRateLimiter` and `SingleFlight`

### Logger

`Logger` writes timestamped, colored request logs to standard error. Use
`LoggerWithConfig` to change the destination or disable timestamps.

```go
r.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
	IncludeTimestamp: false,
	Output:           os.Stdout,
}))
```

### Environment checks

```go
r.Use(middleware.EnvVarChecker("DB_URL", "API_KEY"))
```

For compatibility, a missing variable produces a 500 response and then invokes
the next handler.

### CORS

Use `SimpleCORS` for the permissive defaults:

```go
r.Use(middleware.SimpleCORS())
```

Use `StrictCORS` for a credentialed origin allowlist:

```go
r.Use(middleware.StrictCORS([]string{
	"http://localhost:3000",
	"https://app.example.com",
}))
```

For full control, provide `CORSConfig`:

```go
r.Use(middleware.CORS(middleware.CORSConfig{
	AllowedOrigins: []string{
		"http://localhost:3000",
		"https://*.example.com",
	},
	AllowedMethods: []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodOptions,
	},
	AllowedHeaders:     []string{"Content-Type", "Authorization"},
	ExposedHeaders:     []string{"X-Total-Count"},
	MaxAge:            3600,
	AllowCredentials:  true,
	OptionsPassthrough: false,
	Debug:              false,
}))
```

| Option | Default when using `DefaultCORSConfig` |
| --- | --- |
| `AllowedOrigins` | `["*"]` |
| `AllowedMethods` | GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS |
| `AllowedHeaders` | `["*"]` |
| `ExposedHeaders` | empty |
| `MaxAge` | `0` |
| `AllowCredentials` | `false` |
| `OptionsPassthrough` | `false` |
| `Debug` | `false` |

### WebSockets

`WS` registers a GET route that performs the package's existing upgrade
handshake and passes the hijacked connection to the callback.

```go
r.WS("/ws", func(conn net.Conn, req *http.Request) {
	defer conn.Close()
	// Read and write WebSocket frames on conn.
})
```

Use `WebSocketWithConfig` directly when an origin allowlist or custom origin
check is required.

### ResponseWriterWrapper

`ResponseWriterWrapper` captures a response status code and delegates
`http.Hijacker` and `http.Flusher` to its underlying writer.

```go
wrapped := &middleware.ResponseWriterWrapper{
	ResponseWriter: w,
	StatusCode:     http.StatusOK,
}
next.ServeHTTP(wrapped, req)
log.Printf("status=%d", wrapped.StatusCode)
```

## Shared utilities

`middleware.SharedHTTPClient` is configured with connection pooling and a
10-second timeout. `middleware.SharedAPIRateLimiter` is a process-wide token
bucket intended for external API calls.

## Go versions

The module requires Go 1.24.1 or newer. Go 1.26.5 is the preferred development
toolchain, declared separately in `go.mod`, so consumers on the existing
minimum remain supported.

See the [official Go downloads](https://go.dev/dl/) for toolchain installers.

## Development

Run the local quality checks before submitting a change:

```bash
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
go test -cover ./...
```

Compatibility can be checked with explicit toolchains:

```bash
GOTOOLCHAIN=go1.24.1 go test ./...
GOTOOLCHAIN=go1.25.7 go test ./...
GOTOOLCHAIN=go1.26.5 go test ./...
```

The module has no runtime dependencies outside the Go standard library.

## License

MIT
