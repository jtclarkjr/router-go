# router-go

A small, standard-library HTTP router with route groups, path parameters,
middleware, CORS helpers, request logging, rate limiting, and WebSocket
upgrades. Optional `typed` and `openapi` packages add type-first request
handling, validation, OpenAPI 3.1 generation, and Swagger UI without changing
the core `net/http` API.

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
Path parameters are also installed through `req.SetPathValue`, so new code can
use the standard library's `req.PathValue("name")` directly.

```go
r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
	id := router.URLParam(req, "id")
	filter := router.URLQuery(req, "filter")
	fmt.Fprintf(w, "%s:%s", id, filter)
})
```

Paths ending in `/*` use segment-boundary prefix matching. `/assets/*` matches
`/assets` and `/assets/app.js`, but not `/assets-legacy`.

```go
r.Get("/assets/*", assetHandler)
```

When both `/assets` and `/assets/*` are registered, the exact route handles
`/assets`; the wildcard handles descendants. The same rule applies to an
exact parameter route and a wildcard below it.

### Route groups

`Route` creates a temporary subrouter and copies the middleware already
installed on its parent. The full prefixed path is compiled when the group is
attached.

```go
r.Route("/admin", func(admin *router.Router) {
	admin.Use(requireAdmin)
	admin.Get("/users/{id}", showAdminUser)
})
```

Middleware is wrapped at registration time. Calling `Use` does not alter
routes that were registered earlier.

### Matching, duplicate routes, and 405 responses

Matching is deterministic regardless of registration order: static segments
win over parameters, parameters win over wildcards, and longer wildcard
prefixes win over shorter ones. A request for `/groups/users` therefore cannot
be captured by `/groups/{id}`.

`Handle` and the method helpers retain their existing signatures and panic for
invalid or duplicate registrations. Use `Register` when startup code should
return the error instead:

```go
if err := r.Register(http.MethodGet, "/users/{id}", showUser); err != nil {
	log.Fatal(err)
}
```

Parameter names do not make otherwise identical patterns distinct, so
`GET /users/{id}` conflicts with `GET /users/{name}`. Requests that match a
known path but not its method receive `405 Method Not Allowed` with a sorted
`Allow` header. Unknown paths continue to receive `404 Not Found`.

Different methods may use different names for the same structural parameter
path (for example, `GET /users/{id}` and `POST /users/{userID}`). `HEAD` and
`OPTIONS` are explicit registrations; a GET route does not implicitly install
either method.

`Routes` returns deterministic metadata for diagnostics or integrations:

```go
for _, route := range r.Routes() {
	log.Printf("%s %s", route.Method, route.Path)
}
```

## Typed handlers and OpenAPI 3.1

The optional `typed` package wraps a core router. Request fields declare their
transport location with `path`, `query`, `header`, `cookie`, or `body` tags;
the same types generate the OpenAPI contract.

```go
package main

import (
	"log"
	"net/http"

	router "github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/openapi"
	"github.com/jtclarkjr/router-go/typed"
)

type createMessageBody struct {
	Text string `json:"text" validate:"required,min=1,max=4000"`
}

type createMessageInput struct {
	RoomID string            `path:"roomId"`
	Trace  string            `header:"X-Request-ID" validate:"required"`
	Body   createMessageBody `body:"" validate:"required"`
}

type message struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func main() {
	base := router.NewRouter()
	contract := openapi.New(openapi.Info{Title: "Chat API", Version: "1.0.0"})
	r := typed.New(base, typed.WithRegistry(contract))

	err := typed.Register(r, typed.Operation[createMessageInput, message]{
		Method:        http.MethodPost,
		Path:          "/v1/rooms/{roomId}/messages",
		OperationID:   "createMessage",
		SuccessStatus: http.StatusCreated,
	}, func(req *http.Request, input createMessageInput) (typed.Response[message], error) {
		created := message{ID: "message-1", Text: input.Body.Text}
		return typed.Response[message]{
			Header: http.Header{"X-Request-ID": []string{input.Trace}},
			Body:   created,
		}, nil
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := r.MountDocs("/openapi.json", "/openapi.yaml", "/docs"); err != nil {
		log.Fatal(err)
	}

	log.Fatal(http.ListenAndServe(":8080", r))
}
```

An input struct without transport tags is treated as a direct JSON body.
JSON decoding rejects unknown fields and limits request bodies to 1 MiB by
default; both policies are configurable. Supported validation rules are
`required`, `min`, `max`, `len`, `oneof`, and `regexp`.

Fields with a constraint must declare whether they are `required` or
`omitempty`; this keeps runtime validation and the generated schema aligned.
Generated Go struct schemas are closed with `additionalProperties: false` by
default. `WithUnknownJSONFieldsAllowed` changes both decoding and generated
schemas to allow undeclared properties.

Use `typed.RegisterRaw` with an explicit `openapi.Operation` for endpoints that
must retain raw `net/http` semantics, including SSE, multipart uploads,
reverse proxies, and WebSocket upgrades. Raw handlers are never buffered by
the typed codec. See [Typed handlers and contracts](docs/typed-openapi.md) for
the complete API.

The generated `/openapi.yaml` is deterministic YAML 1.2. Both serializers stay
dependency-free.

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

The module, including its typed and OpenAPI packages, has no runtime
dependencies outside the Go standard library.

## License

MIT
