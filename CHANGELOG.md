# Changelog

## [0.7.0] - Unreleased

### Added

- Error-returning `Router.Register` and deterministic `Router.Routes`
  introspection.
- Standard-library path values through `http.Request.PathValue` while keeping
  `router.URLParam` compatible.
- Optional `typed` package with generic request/response handlers, transport
  binding, validation, configurable error codecs, response status/headers, and
  raw documented handlers.
- Optional `openapi` package with OpenAPI 3.1 and JSON Schema generation,
  deterministic JSON/YAML output, document handlers, and Swagger UI.
- Concurrency, precedence, contract, codec, raw streaming, race, and golden
  document tests.

### Changed

- Route lookup is deterministic: static segments precede parameters, which
  precede wildcards; longer wildcard prefixes are considered first.
- Exact routes precede wildcards that also match the wildcard base, and
  literal-constrained parameter segments precede generic parameters.
- Paths ending in `/*` match on a segment boundary instead of matching similar
  prefixes such as `/assets-legacy`.
- Prefixed route groups compile and dispatch their full path correctly.
- Requests for an existing path with an unsupported method return 405 and a
  sorted `Allow` header instead of 404.
- Invalid, exact duplicate, and structurally duplicate route registrations are
  rejected. Existing `Handle` and method helpers panic at startup; callers can
  use `Register` to handle the error.
- Struct contracts are closed by default, component names are stable across
  registration order, and parameter validation constraints are emitted into
  OpenAPI.
- Invalid, non-finite, ambiguous-length, and non-canonical enum constraints
  are rejected before route registration.

### Compatibility

Existing core imports, method-helper signatures, middleware signatures,
`Route` fields, `URLParam`, `URLQuery`, and WebSocket registration remain
source-compatible. The matching changes above intentionally correct
nondeterministic or boundary-unsafe behavior.
