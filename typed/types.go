package typed

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	router "github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/openapi"
)

// Empty is a convenient request type for operations without input.
type Empty struct{}

// NoBody is a convenient response type for operations without a body.
type NoBody struct{}

// Response is a typed HTTP response. Status defaults to the operation's
// SuccessStatus and Header values are copied before the response is written.
type Response[T any] struct {
	Status int
	Header http.Header
	Body   T
}

// Handler handles a decoded and validated typed request.
type Handler[I, O any] func(*http.Request, I) (Response[O], error)

// Operation describes a typed HTTP operation and its public contract.
type Operation[I, O any] struct {
	Method              string
	Path                string
	OperationID         string
	Summary             string
	Description         string
	Tags                []string
	Deprecated          bool
	SuccessStatus       int
	SuccessDescription  string
	RequestContentType  string
	ResponseContentType string
	ResponseHeaders     map[string]openapi.Header
	AdditionalResponses map[int]openapi.Response
	Security            []openapi.SecurityRequirement
}

// RawKind records why an operation uses the raw net/http escape hatch.
type RawKind string

const (
	RawHTTP      RawKind = "http"
	RawSSE       RawKind = "sse"
	RawMultipart RawKind = "multipart"
	RawProxy     RawKind = "proxy"
	RawWebSocket RawKind = "websocket"
)

// RawOperation registers an ordinary http.Handler with an explicit contract.
// It is intended for streaming, multipart, reverse-proxy, and WebSocket
// endpoints that should be documented but not buffered by the typed codec.
type RawOperation struct {
	Method       string
	Path         string
	DocumentPath string
	Kind         RawKind
	Spec         openapi.Operation
}

// Error is an application error understood by DefaultErrorCodec.
type Error struct {
	Status  int
	Code    string
	Message string
	Details any
	Cause   error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return http.StatusText(e.StatusCode())
}

// Unwrap exposes the underlying cause.
func (e *Error) Unwrap() error { return e.Cause }

// StatusCode returns a valid HTTP status, defaulting to 500.
func (e *Error) StatusCode() int {
	if e.Status >= 400 && e.Status <= 599 {
		return e.Status
	}
	return http.StatusInternalServerError
}

// ErrorCode returns the stable machine-readable error code.
func (e *Error) ErrorCode() string {
	if e.Code != "" {
		return e.Code
	}
	return "internal_error"
}

// ErrorDetails returns structured error details.
func (e *Error) ErrorDetails() any { return e.Details }

// FieldViolation identifies one failed input binding or validation rule.
type FieldViolation struct {
	Field   string `json:"field"`
	In      string `json:"in"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// RequestError is returned when typed request binding or validation fails.
type RequestError struct {
	Status     int
	Code       string
	Message    string
	Violations []FieldViolation
	Cause      error
}

func (e *RequestError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "invalid request"
}

// Unwrap exposes the binding or validation cause.
func (e *RequestError) Unwrap() error { return e.Cause }

// StatusCode implements the status-coder convention used by the default
// error codec.
func (e *RequestError) StatusCode() int {
	if e.Status >= 400 && e.Status <= 499 {
		return e.Status
	}
	return http.StatusBadRequest
}

// ErrorCode returns the stable machine-readable request error code.
func (e *RequestError) ErrorCode() string {
	if e.Code != "" {
		return e.Code
	}
	return "invalid_request"
}

// ErrorDetails returns field-level violations.
func (e *RequestError) ErrorDetails() any {
	if len(e.Violations) == 0 {
		return nil
	}
	return e.Violations
}

// ErrorCodec writes handler, binding, validation, and encoding errors.
type ErrorCodec interface {
	WriteError(http.ResponseWriter, *http.Request, error)
}

// ErrorCodecFunc adapts a function into ErrorCodec.
type ErrorCodecFunc func(http.ResponseWriter, *http.Request, error)

func (f ErrorCodecFunc) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	f(w, r, err)
}

type statusCoder interface{ StatusCode() int }
type codeCoder interface{ ErrorCode() string }
type detailsCoder interface{ ErrorDetails() any }

// DefaultErrorCodec writes a stable JSON error envelope and does not expose
// unknown internal error messages.
var DefaultErrorCodec ErrorCodec = ErrorCodecFunc(func(w http.ResponseWriter, _ *http.Request, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "internal server error"
	var details any
	var applicationError *Error
	errors.As(err, &applicationError)

	var withStatus statusCoder
	if errors.As(err, &withStatus) {
		candidate := withStatus.StatusCode()
		if candidate >= 400 && candidate <= 599 {
			status = candidate
			if status < 500 {
				if applicationError != nil {
					if applicationError.Message != "" {
						message = applicationError.Message
					} else {
						message = http.StatusText(status)
						if message == "" {
							message = "request failed"
						}
					}
				} else {
					message = err.Error()
				}
			}
		}
	}
	var withCode codeCoder
	if errors.As(err, &withCode) {
		if candidate := withCode.ErrorCode(); candidate != "" {
			code = candidate
		}
	}
	var withDetails detailsCoder
	if status < 500 && errors.As(err, &withDetails) {
		details = withDetails.ErrorDetails()
	}

	payload := map[string]any{
		"error": message,
		"code":  code,
	}
	if details != nil {
		payload["details"] = details
	}
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		encoded = []byte(`{"error":"internal server error","code":"internal_error"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
})

// Router combines a core router with a typed codec and OpenAPI registry.
type Router struct {
	base                  *router.Router
	registry              *openapi.Registry
	errorCodec            ErrorCodec
	maxBodyBytes          int64
	disallowUnknownFields bool
}

// Option configures a typed Router.
type Option func(*Router)

// WithRegistry uses registry for generated contracts.
func WithRegistry(registry *openapi.Registry) Option {
	return func(r *Router) {
		if registry != nil {
			r.registry = registry
		}
	}
}

// WithErrorCodec replaces the default JSON error codec.
func WithErrorCodec(codec ErrorCodec) Option {
	return func(r *Router) {
		if codec != nil {
			r.errorCodec = codec
		}
	}
}

// WithMaxBodyBytes changes the JSON request-body limit. Values <= 0 disable
// the limit. The default is 1 MiB.
func WithMaxBodyBytes(limit int64) Option {
	return func(r *Router) { r.maxBodyBytes = limit }
}

// WithUnknownJSONFieldsAllowed permits fields not declared by the body type.
// Unknown fields are rejected by default.
func WithUnknownJSONFieldsAllowed() Option {
	return func(r *Router) { r.disallowUnknownFields = false }
}

// New wraps base with typed registration. A new core router is created when
// base is nil.
func New(base *router.Router, options ...Option) *Router {
	if base == nil {
		base = router.NewRouter()
	}
	r := &Router{
		base:                  base,
		registry:              openapi.New(openapi.Info{Title: "API", Version: "0.0.0"}),
		errorCodec:            DefaultErrorCodec,
		maxBodyBytes:          1 << 20,
		disallowUnknownFields: true,
	}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	r.registry.SetStructAdditionalPropertiesAllowed(!r.disallowUnknownFields)
	return r
}

// Base returns the underlying core router.
func (r *Router) Base() *router.Router { return r.base }

// Registry returns the OpenAPI registry.
func (r *Router) Registry() *openapi.Registry { return r.registry }

// ServeHTTP dispatches through the underlying core router.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.base.ServeHTTP(w, req)
}

// MountDocs registers JSON, YAML, and Swagger UI handlers. Empty paths skip
// the corresponding handler.
func (r *Router) MountDocs(jsonPath, yamlPath, uiPath string) error {
	registrations := []struct {
		path    string
		handler http.Handler
	}{
		{jsonPath, r.registry.JSONHandler()},
		{yamlPath, r.registry.YAMLHandler()},
		{uiPath, openapi.SwaggerUI(jsonPath, r.registry.Document().Info.Title)},
	}
	for _, registration := range registrations {
		if registration.path == "" {
			continue
		}
		if err := r.base.Register(http.MethodGet, registration.path, registration.handler); err != nil {
			return fmt.Errorf("mount typed docs at %s: %w", registration.path, err)
		}
	}
	return nil
}
