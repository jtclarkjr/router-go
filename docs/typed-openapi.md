# Typed handlers and generated contracts

The `typed` and `openapi` packages are optional layers over the core
`github.com/jtclarkjr/router-go` package. Importing them does not change the
core router's middleware or handler model.

## Registration API

```go
func Register[I, O any](
	r *typed.Router,
	op typed.Operation[I, O],
	h typed.Handler[I, O],
) error

type Handler[I, O any] func(
	*http.Request,
	I,
) (typed.Response[O], error)
```

`typed.Response` contains `Status`, `Header`, and `Body`. A zero status uses
the operation's declared success status. `typed.NoBody` defaults that status
to 204 and suppresses the body. Other outputs default to 200 and JSON.

Registration first validates and documents the operation, then installs its
HTTP handler. A failed HTTP registration removes the operation from the
contract registry.

## Request binding

An input can be a direct JSON body or a transport wrapper:

```go
type updateInput struct {
	ID      string      `path:"id"`
	DryRun  bool        `query:"dryRun"`
	TraceID string      `header:"X-Trace-ID" validate:"required"`
	Session *string     `cookie:"session"`
	Body    updateBody  `body:"" validate:"required"`
	Actor   currentUser `transport:"-"`
}
```

- Every `{parameter}` in the route must have exactly one `path` field.
- An exported wrapper field must have one transport tag or `transport:"-"`.
- Only one `body` field is allowed.
- A non-pointer `body` field is required; use a pointer for an optional body.
- A struct with no transport tags is decoded as the complete JSON body.
- Parameter values support strings, booleans, integers, unsigned integers,
  floats, slices, pointers, and `encoding.TextUnmarshaler` implementations.
- JSON defaults to a 1 MiB limit and rejects unknown fields. Configure these
  with `WithMaxBodyBytes` and `WithUnknownJSONFieldsAllowed`.
- Typed handlers support `application/json` and structured `+json` media
  types. Use a raw operation for other codecs or multipart bodies.

Validation tags support `required`, `min`, `max`, `len`, `oneof`, and
`regexp`. A request type can also implement either `Validate() error` or
`Validate(*http.Request) error`.

A field with `min`, `max`, `len`, `oneof`, or `regexp` must also use either
`required` or `omitempty`. Validation recursively checks struct values inside
arrays, slices, and maps. This makes the generated required/constraint rules
match request handling rather than relying on a Go zero value to imply
presence.

Length constraints on strings and collections use non-negative integers.
Numeric and boolean `oneof` values use their canonical Go representation (for
example, `true`, not `TRUE`), keeping runtime comparisons and JSON Schema enum
values identical. Non-finite numeric constraints are rejected at registration.

Generated struct schemas use `additionalProperties: false` because unknown
JSON fields are rejected by default. `WithUnknownJSONFieldsAllowed` updates
the shared registry policy as well as the decoder.

## Errors

The default codec returns:

```json
{
  "error": "request validation failed",
  "code": "validation_error",
  "details": [
    {
      "field": "limit",
      "in": "query",
      "rule": "min",
      "message": "must be at least 1"
    }
  ]
}
```

Application errors can implement `StatusCode() int`, `ErrorCode() string`, and
`ErrorDetails() any`, or use `typed.Error`. Unknown errors are returned as a
generic 500 without leaking their messages. Replace the complete policy with
`typed.WithErrorCodec`.

The default codec accepts only 4xx/5xx values from `StatusCode`. It hides 5xx
messages, causes, and details; applications that intentionally expose another
policy must opt into a custom codec.

## Raw documented operations

`RegisterRaw` keeps the handler on the ordinary `net/http` path while adding
an explicit OpenAPI operation:

```go
err := typed.RegisterRaw(r, typed.RawOperation{
	Method: http.MethodGet,
	Path:   "/v1/events",
	Kind:   typed.RawSSE,
	Spec: openapi.Operation{
		OperationID: "events",
		Responses: map[string]openapi.Response{
			"200": {
				Description: "Event stream",
				Content: map[string]openapi.MediaType{
					"text/event-stream": {
						Schema: &openapi.Schema{Type: "string"},
					},
				},
			},
		},
	},
}, streamHandler)
```

Kinds are `RawHTTP`, `RawSSE`, `RawMultipart`, `RawProxy`, and
`RawWebSocket`. They are emitted as the `x-router-kind` OpenAPI extension and
do not alter runtime behavior. When the runtime route uses a router wildcard,
set `Path: "/proxy/*"` and `DocumentPath: "/proxy/{path}"`; OpenAPI receives
the parameterized document path and the runtime path is retained in the
`x-router-runtime-path` extension.

## OpenAPI registry

`openapi.New` creates a concurrency-safe OpenAPI 3.1 registry. It supports:

- operation registration and duplicate detection;
- reusable schemas generated from Go structs;
- recursive types, pointers/nullability, arrays, maps, numeric formats, and
  validation constraints;
- servers, tags, and security schemes;
- deterministic, dependency-free JSON and YAML 1.2 serialization;
- live JSON/YAML handlers and Swagger UI.

Swagger UI loads major-version-constrained browser assets from
`swagger-ui-dist@5`; the API document itself remains served by the
application.

Go struct component names include a stable type-identity suffix. This keeps
generic instantiations, function-local types, and same-named types from
different packages valid and independent of registration order. Security
schemes are validated when added, and operation security requirements must
reference a registered scheme.
The dependency-free security model supports `http`, `apiKey`, and `mutualTLS`;
use an explicit contract layer if OAuth flows or OpenID Connect metadata are
required.

Schema reflection targets ordinary `encoding/json` struct behavior. Types that
replace it with custom `MarshalJSON`/`UnmarshalJSON` methods, fields using the
special `json:",string"` representation, or deliberately ambiguous embedded
JSON fields should use `RegisterRaw` with an explicit schema.
