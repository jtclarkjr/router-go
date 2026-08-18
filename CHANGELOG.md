# Changelog

## [0.8.0] - 2026-08-18

### Added

- `Router.RegisterAlias` for runtime aliases that reuse an existing route's
  handler and captured middleware without double-wrapping global middleware.
- Optional per-operation middleware through additive typed and raw
  registration functions, allowing authentication and request enrichment to
  run before typed binding without changing existing operation structs.
- Structured request logging through `SlogLogger` and
  `SlogLoggerWithConfig`.
- Startup environment validation through `MissingEnvVars` and
  `RequireEnvVars`.
- `RecovererWithHandler` for application-owned panic logging and response
  formats.

### Compatibility

- All additions are opt-in. Existing route matching, registration,
  middleware ordering, logger output, recovery responses, and
  `EnvVarChecker` behavior are unchanged.

## [0.7.1] - 2026-08-17

### Fixed

- Required pointer and interface fields now mean present/non-null in generated
  schemas, matching runtime validation, without incorrectly imposing nonzero
  constraints on their underlying scalar values.

## [0.7.0] - 2026-08-17

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
