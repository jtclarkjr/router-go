package typed_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	router "github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/openapi"
	"github.com/jtclarkjr/router-go/typed"
)

type createMessageBody struct {
	Text string `json:"text" validate:"required,min=2,max=100" description:"Message text"`
}

type createMessageInput struct {
	RoomID    string            `path:"roomId" validate:"required" description:"Room identifier"`
	Limit     int               `query:"limit" validate:"required,min=1,max=100"`
	RequestID string            `header:"X-Request-ID" validate:"required"`
	Session   *string           `cookie:"session"`
	Body      createMessageBody `body:"" validate:"required"`
}

type messageOutput struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type invalidErrorStatus struct{}

func (invalidErrorStatus) Error() string   { return "must not be a success" }
func (invalidErrorStatus) StatusCode() int { return http.StatusOK }

func TestRegisterBindsValidatesAndDocuments(t *testing.T) {
	base := router.NewRouter()
	registry := openapi.New(openapi.Info{Title: "Chat", Version: "0.7.0"})
	r := typed.New(base, typed.WithRegistry(registry))

	err := typed.Register(r, typed.Operation[createMessageInput, messageOutput]{
		Method:             http.MethodPost,
		Path:               "/rooms/{roomId}/messages",
		OperationID:        "createMessage",
		Summary:            "Create a message",
		Tags:               []string{"messages"},
		SuccessStatus:      http.StatusCreated,
		SuccessDescription: "Message created",
		ResponseHeaders: map[string]openapi.Header{
			"X-Request-ID": {Schema: &openapi.Schema{Type: "string"}},
		},
	}, func(_ *http.Request, input createMessageInput) (typed.Response[messageOutput], error) {
		if input.RoomID != "room-1" || input.Limit != 25 || input.RequestID != "request-1" {
			return typed.Response[messageOutput]{}, fmt.Errorf("unexpected input: %#v", input)
		}
		if input.Session == nil || *input.Session != "session-1" {
			return typed.Response[messageOutput]{}, fmt.Errorf("unexpected cookie: %#v", input.Session)
		}
		return typed.Response[messageOutput]{
			Header: http.Header{"X-Request-ID": []string{input.RequestID}},
			Body:   messageOutput{ID: "message-1", Text: input.Body.Text},
		}, nil
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/rooms/room-1/messages?limit=25", strings.NewReader(`{"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "request-1")
	req.AddCookie(&http.Cookie{Name: "session", Value: "session-1"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := rec.Header().Get("X-Request-ID"); got != "request-1" {
		t.Fatalf("response header = %q, want request-1", got)
	}
	if got := rec.Body.String(); got != `{"id":"message-1","text":"hello"}` {
		t.Fatalf("body = %s", got)
	}

	document := registry.Document()
	operation := document.Paths["/rooms/{roomId}/messages"]["post"]
	if operation.OperationID != "createMessage" || operation.RequestBody == nil {
		t.Fatalf("operation = %#v", operation)
	}
	if len(operation.Parameters) != 4 {
		t.Fatalf("parameters = %d, want 4", len(operation.Parameters))
	}
	for _, parameter := range operation.Parameters {
		if parameter.Name == "limit" {
			if parameter.Schema.Minimum == nil || *parameter.Schema.Minimum != 1 || parameter.Schema.Maximum == nil || *parameter.Schema.Maximum != 100 {
				t.Fatalf("limit schema constraints = %#v", parameter.Schema)
			}
		}
	}
	if _, ok := operation.Responses["201"]; !ok {
		t.Fatalf("responses = %#v, want 201", operation.Responses)
	}
	if len(document.Components.Schemas) != 2 {
		t.Fatalf("component schemas = %#v, want request and response schemas", document.Components.Schemas)
	}
	requestRef := operation.RequestBody.Content["application/json"].Schema.Ref
	requestSchema := document.Components.Schemas[strings.TrimPrefix(requestRef, "#/components/schemas/")]
	if requestSchema == nil || requestSchema.AdditionalPropertiesAllowed == nil || *requestSchema.AdditionalPropertiesAllowed {
		t.Fatalf("request schema additionalProperties = %#v, want false", requestSchema)
	}
}

func TestValidationAndBindingErrorsUseStableEnvelope(t *testing.T) {
	r := typed.New(router.NewRouter())
	typed.MustRegister(r, typed.Operation[createMessageInput, messageOutput]{
		Method: http.MethodPost,
		Path:   "/rooms/{roomId}/messages",
	}, func(_ *http.Request, input createMessageInput) (typed.Response[messageOutput], error) {
		return typed.Response[messageOutput]{Body: messageOutput{Text: input.Body.Text}}, nil
	})

	tests := []struct {
		name        string
		path        string
		body        string
		contentType string
		requestID   string
		status      int
		code        string
	}{
		{name: "missing required header", path: "/rooms/1/messages?limit=2", body: `{"text":"ok"}`, contentType: "application/json", status: 400, code: "validation_error"},
		{name: "query format", path: "/rooms/1/messages?limit=nope", body: `{"text":"ok"}`, contentType: "application/json", requestID: "id", status: 400, code: "binding_error"},
		{name: "body validation", path: "/rooms/1/messages?limit=2", body: `{"text":"x"}`, contentType: "application/json", requestID: "id", status: 400, code: "validation_error"},
		{name: "unknown JSON", path: "/rooms/1/messages?limit=2", body: `{"text":"ok","unknown":true}`, contentType: "application/json", requestID: "id", status: 400, code: "invalid_json"},
		{name: "unsupported content", path: "/rooms/1/messages?limit=2", body: `{"text":"ok"}`, contentType: "text/plain", requestID: "id", status: 415, code: "unsupported_media_type"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			req.Header.Set("Content-Type", test.contentType)
			if test.requestID != "" {
				req.Header.Set("X-Request-ID", test.requestID)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, test.status, rec.Body.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if payload["code"] != test.code {
				t.Fatalf("code = %v, want %s", payload["code"], test.code)
			}
		})
	}
}

type directBody struct {
	Name string `json:"name" validate:"required"`
}

func TestDirectBodyNoBodyAndCustomCodec(t *testing.T) {
	called := false
	codec := typed.ErrorCodecFunc(func(w http.ResponseWriter, _ *http.Request, err error) {
		called = true
		http.Error(w, "custom: "+err.Error(), http.StatusTeapot)
	})
	r := typed.New(router.NewRouter(), typed.WithErrorCodec(codec))
	typed.MustRegister(r, typed.Operation[directBody, typed.NoBody]{
		Method: http.MethodDelete,
		Path:   "/resource",
	}, func(_ *http.Request, input directBody) (typed.Response[typed.NoBody], error) {
		if input.Name == "fail" {
			return typed.Response[typed.NoBody]{}, errors.New("failed")
		}
		return typed.Response[typed.NoBody]{}, nil
	})

	success := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/resource", strings.NewReader(`{"name":"ok"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(success, req)
	if success.Code != http.StatusNoContent || success.Body.Len() != 0 {
		t.Fatalf("success = %d %q, want 204 empty", success.Code, success.Body.String())
	}

	failure := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/resource", strings.NewReader(`{"name":"fail"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(failure, req)
	if !called || failure.Code != http.StatusTeapot {
		t.Fatalf("custom codec called=%v status=%d", called, failure.Code)
	}
}

func TestDefaultErrorCodecRejectsInvalidStatusesAndHidesInternalCauses(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "invalid status", err: invalidErrorStatus{}},
		{name: "internal cause", err: &typed.Error{
			Status:  http.StatusInternalServerError,
			Code:    "database_failed",
			Cause:   errors.New("database password leaked"),
			Details: map[string]string{"secret": "leaked"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			typed.DefaultErrorCodec.WriteError(rec, httptest.NewRequest(http.MethodGet, "/", nil), test.err)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			if body := rec.Body.String(); strings.Contains(body, "password") || strings.Contains(body, "leaked") {
				t.Fatalf("internal error leaked in body: %s", body)
			}
		})
	}
}

func TestResponseEncodingFailureDoesNotLeakSuccessHeaders(t *testing.T) {
	r := typed.New(router.NewRouter())
	typed.MustRegister(r, typed.Operation[typed.Empty, any]{
		Method: http.MethodGet,
		Path:   "/encoding-failure",
	}, func(*http.Request, typed.Empty) (typed.Response[any], error) {
		return typed.Response[any]{
			Header: http.Header{"X-Success": []string{"must-not-leak"}},
			Body:   make(chan int),
		}, nil
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/encoding-failure", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Success"); got != "" {
		t.Fatalf("success header leaked into error response: %q", got)
	}
}

func TestBodyLimitAndUnknownFieldOption(t *testing.T) {
	r := typed.New(router.NewRouter(), typed.WithMaxBodyBytes(24), typed.WithUnknownJSONFieldsAllowed())
	typed.MustRegister(r, typed.Operation[directBody, directBody]{Method: http.MethodPost, Path: "/body"},
		func(_ *http.Request, input directBody) (typed.Response[directBody], error) {
			return typed.Response[directBody]{Body: input}, nil
		})
	document := r.Registry().Document()
	requestRef := document.Paths["/body"]["post"].RequestBody.Content["application/json"].Schema.Ref
	requestSchema := document.Components.Schemas[strings.TrimPrefix(requestRef, "#/components/schemas/")]
	if requestSchema.AdditionalPropertiesAllowed == nil || !*requestSchema.AdditionalPropertiesAllowed {
		t.Fatalf("open request schema additionalProperties = %#v, want true", requestSchema.AdditionalPropertiesAllowed)
	}

	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader(`{"name":"x","z":1}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(allowed, req)
	if allowed.Code != http.StatusOK {
		t.Fatalf("unknown field option status = %d; body=%s", allowed.Code, allowed.Body.String())
	}

	tooLarge := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/body", strings.NewReader(`{"name":"this body is too large"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(tooLarge, req)
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body status = %d, want 413; body=%s", tooLarge.Code, tooLarge.Body.String())
	}
}

func TestStructuredJSONMediaTypeAndOptionalPointerBody(t *testing.T) {
	type optionalInput struct {
		Body *directBody `body:""`
	}

	r := typed.New(router.NewRouter())
	typed.MustRegister(r, typed.Operation[directBody, directBody]{
		Method:              http.MethodPost,
		Path:                "/vendor-json",
		RequestContentType:  "application/vnd.tessuract+json",
		ResponseContentType: "application/vnd.tessuract+json",
	}, func(_ *http.Request, input directBody) (typed.Response[directBody], error) {
		return typed.Response[directBody]{Body: input}, nil
	})
	typed.MustRegister(r, typed.Operation[optionalInput, typed.NoBody]{
		Method: http.MethodPost,
		Path:   "/optional",
	}, func(_ *http.Request, input optionalInput) (typed.Response[typed.NoBody], error) {
		if input.Body != nil {
			return typed.Response[typed.NoBody]{}, errors.New("optional body was allocated")
		}
		return typed.Response[typed.NoBody]{}, nil
	})

	vendor := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vendor-json", strings.NewReader(`{"name":"ok"}`))
	req.Header.Set("Content-Type", "application/vnd.tessuract+json; profile=test")
	r.ServeHTTP(vendor, req)
	if vendor.Code != http.StatusOK || vendor.Header().Get("Content-Type") != "application/vnd.tessuract+json; charset=utf-8" {
		t.Fatalf("vendor response = %d %q; body=%s", vendor.Code, vendor.Header().Get("Content-Type"), vendor.Body.String())
	}

	optional := httptest.NewRecorder()
	r.ServeHTTP(optional, httptest.NewRequest(http.MethodPost, "/optional", nil))
	if optional.Code != http.StatusNoContent {
		t.Fatalf("optional body response = %d; body=%s", optional.Code, optional.Body.String())
	}
}

func TestHeadOperationSuppressesRuntimeAndDocumentedBody(t *testing.T) {
	r := typed.New(router.NewRouter())
	typed.MustRegister(r, typed.Operation[typed.Empty, messageOutput]{
		Method: "head",
		Path:   "/messages/latest",
	}, func(*http.Request, typed.Empty) (typed.Response[messageOutput], error) {
		return typed.Response[messageOutput]{Body: messageOutput{ID: "message-1"}}, nil
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("head", "/messages/latest", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD response = %d %q, want 200 empty", rec.Code, rec.Body.String())
	}
	response := r.Registry().Document().Paths["/messages/latest"]["head"].Responses["200"]
	if len(response.Content) != 0 {
		t.Fatalf("HEAD response documents a body: %#v", response.Content)
	}
}

func TestTypedRegistrationRejectsNonJSONCodec(t *testing.T) {
	r := typed.New(router.NewRouter())
	err := typed.Register(r, typed.Operation[directBody, directBody]{
		Method:             http.MethodPost,
		Path:               "/xml",
		RequestContentType: "application/xml",
	}, func(_ *http.Request, input directBody) (typed.Response[directBody], error) {
		return typed.Response[directBody]{Body: input}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "RegisterRaw") {
		t.Fatalf("non-JSON registration error = %v", err)
	}
}

func TestRegisterRawDocumentsSSEWithoutBuffering(t *testing.T) {
	r := typed.New(router.NewRouter())
	err := typed.RegisterRaw(r, typed.RawOperation{
		Method: http.MethodGet,
		Path:   "/events",
		Kind:   typed.RawSSE,
		Spec: openapi.Operation{
			OperationID: "events",
			Responses: map[string]openapi.Response{
				"200": {
					Description: "Event stream",
					Content: map[string]openapi.MediaType{
						"text/event-stream": {Schema: &openapi.Schema{Type: "string"}},
					},
				},
			},
		},
	}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: ready\n\n")
	}))
	if err != nil {
		t.Fatalf("RegisterRaw: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events", nil))
	if rec.Body.String() != "data: ready\n\n" {
		t.Fatalf("stream body = %q", rec.Body.String())
	}
	document, err := r.Registry().JSON()
	if err != nil {
		t.Fatalf("OpenAPI JSON: %v", err)
	}
	if !strings.Contains(string(document), `"x-router-kind": "sse"`) {
		t.Fatalf("raw kind missing from document:\n%s", document)
	}
}

func TestRegisterRawSeparatesWildcardRuntimeAndDocumentPaths(t *testing.T) {
	r := typed.New(router.NewRouter())
	err := typed.RegisterRaw(r, typed.RawOperation{
		Method:       http.MethodGet,
		Path:         "/proxy/*",
		DocumentPath: "/proxy/{path}",
		Kind:         typed.RawProxy,
		Spec: openapi.Operation{
			Parameters: []openapi.Parameter{{Name: "path", In: "path", Required: true, Schema: &openapi.Schema{Type: "string"}}},
			Responses: map[string]openapi.Response{
				"200": {Description: "Proxied response"},
			},
		},
	}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprint(w, req.URL.Path)
	}))
	if err != nil {
		t.Fatalf("RegisterRaw proxy: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/proxy/upstream/path", nil))
	if got := rec.Body.String(); got != "/proxy/upstream/path" {
		t.Fatalf("proxy body = %q", got)
	}
	operation := r.Registry().Document().Paths["/proxy/{path}"]["get"]
	if got := operation.Extensions["x-router-runtime-path"]; got != "/proxy/*" {
		t.Fatalf("runtime path extension = %#v", got)
	}
}

func TestRegistrationRollbackAndInputContractErrors(t *testing.T) {
	r := typed.New(router.NewRouter())
	typed.MustRegister(r, typed.Operation[typed.Empty, typed.NoBody]{Method: http.MethodGet, Path: "/duplicate"},
		func(*http.Request, typed.Empty) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})

	err := typed.Register(r, typed.Operation[typed.Empty, typed.NoBody]{Method: http.MethodGet, Path: "/duplicate"},
		func(*http.Request, typed.Empty) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})
	if !errors.Is(err, openapi.ErrDuplicateOperation) {
		t.Fatalf("duplicate error = %v, want OpenAPI duplicate", err)
	}

	type invalidInput struct {
		Missing string
		ID      string `path:"id"`
	}
	err = typed.Register(r, typed.Operation[invalidInput, typed.NoBody]{Method: http.MethodGet, Path: "/invalid/{id}"},
		func(*http.Request, invalidInput) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "needs path, query, header") {
		t.Fatalf("invalid input error = %v", err)
	}

	type invalidValidation struct {
		Value string `query:"value" validate:"unknown"`
	}
	err = typed.Register(r, typed.Operation[invalidValidation, typed.NoBody]{Method: http.MethodGet, Path: "/invalid-validation"},
		func(*http.Request, invalidValidation) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "unknown validation rule") {
		t.Fatalf("invalid validation error = %v", err)
	}

	type ambiguousValidation struct {
		Value int `query:"value" validate:"min=1"`
	}
	err = typed.Register(r, typed.Operation[ambiguousValidation, typed.NoBody]{Method: http.MethodGet, Path: "/ambiguous-validation"},
		func(*http.Request, ambiguousValidation) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "require required or omitempty") {
		t.Fatalf("ambiguous validation error = %v", err)
	}

	type unsupportedParameter struct {
		Value map[string]string `query:"value"`
	}
	err = typed.Register(r, typed.Operation[unsupportedParameter, typed.NoBody]{Method: http.MethodGet, Path: "/unsupported-parameter"},
		func(*http.Request, unsupportedParameter) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "unsupported map[string]string parameter type") {
		t.Fatalf("unsupported parameter error = %v", err)
	}

	type nonFiniteConstraint struct {
		Value float64 `query:"value" validate:"required,min=NaN"`
	}
	err = typed.Register(r, typed.Operation[nonFiniteConstraint, typed.NoBody]{Method: http.MethodGet, Path: "/non-finite"},
		func(*http.Request, nonFiniteConstraint) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "invalid value") {
		t.Fatalf("non-finite validation error = %v", err)
	}

	type fractionalLength struct {
		Value string `query:"value" validate:"required,min=1.5"`
	}
	err = typed.Register(r, typed.Operation[fractionalLength, typed.NoBody]{Method: http.MethodGet, Path: "/fractional-length"},
		func(*http.Request, fractionalLength) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "non-negative integer") {
		t.Fatalf("fractional length validation error = %v", err)
	}

	type nonCanonicalEnum struct {
		Value bool `query:"value" validate:"required,oneof=TRUE false"`
	}
	err = typed.Register(r, typed.Operation[nonCanonicalEnum, typed.NoBody]{Method: http.MethodGet, Path: "/non-canonical-enum"},
		func(*http.Request, nonCanonicalEnum) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "canonical value") {
		t.Fatalf("non-canonical enum validation error = %v", err)
	}
}

func TestValidationTraversesCollectionElements(t *testing.T) {
	type child struct {
		Name string `json:"name" validate:"required"`
	}
	type collection struct {
		Children []child `json:"children" validate:"required"`
	}
	r := typed.New(router.NewRouter())
	typed.MustRegister(r, typed.Operation[collection, typed.NoBody]{Method: http.MethodPost, Path: "/collection"},
		func(*http.Request, collection) (typed.Response[typed.NoBody], error) {
			return typed.Response[typed.NoBody]{}, nil
		})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/collection", strings.NewReader(`{"children":[{}]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "children[0].name") {
		t.Fatalf("nested validation response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestCollectionParametersFollowOpenAPIDefaultSerialization(t *testing.T) {
	type collectionInput struct {
		IDs     []int    `path:"ids"`
		Tags    []string `query:"tag"`
		Traces  []string `header:"X-Trace"`
		Numbers []int    `cookie:"number"`
	}
	r := typed.New(router.NewRouter())
	typed.MustRegister(r, typed.Operation[collectionInput, typed.NoBody]{
		Method: http.MethodGet,
		Path:   "/items/{ids}",
	}, func(_ *http.Request, input collectionInput) (typed.Response[typed.NoBody], error) {
		if fmt.Sprint(input.IDs) != "[1 2]" || fmt.Sprint(input.Tags) != "[red blue]" ||
			fmt.Sprint(input.Traces) != "[trace-a trace-b]" || fmt.Sprint(input.Numbers) != "[3 4]" {
			return typed.Response[typed.NoBody]{}, fmt.Errorf("unexpected collection parameters: %#v", input)
		}
		return typed.Response[typed.NoBody]{}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/items/1,2?tag=red&tag=blue", nil)
	req.Header.Set("X-Trace", "trace-a,trace-b")
	req.AddCookie(&http.Cookie{Name: "number", Value: "3"})
	req.AddCookie(&http.Cookie{Name: "number", Value: "4"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("collection parameter response = %d %s", rec.Code, rec.Body.String())
	}

	for _, parameter := range r.Registry().Document().Paths["/items/{ids}"]["get"].Parameters {
		if parameter.Schema == nil || parameter.Schema.Type != "array" {
			t.Fatalf("collection parameter schema = %#v, want array", parameter)
		}
	}
}

func TestAdditionalResponseCannotReplaceTypedSuccessContract(t *testing.T) {
	r := typed.New(router.NewRouter())
	err := typed.Register(r, typed.Operation[typed.Empty, messageOutput]{
		Method:        http.MethodGet,
		Path:          "/conflicting-response",
		SuccessStatus: http.StatusOK,
		AdditionalResponses: map[int]openapi.Response{
			http.StatusOK: {Description: "Different response"},
		},
	}, func(*http.Request, typed.Empty) (typed.Response[messageOutput], error) {
		return typed.Response[messageOutput]{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts with the success response") {
		t.Fatalf("registration error = %v", err)
	}
	if len(r.Registry().Document().Components.Schemas) != 0 {
		t.Fatal("failed response validation generated component schemas")
	}
}

func TestMountDocs(t *testing.T) {
	r := typed.New(router.NewRouter(), typed.WithRegistry(openapi.New(openapi.Info{Title: "Docs", Version: "1"})))
	if err := r.MountDocs("/openapi.json", "/openapi.yaml", "/docs"); err != nil {
		t.Fatalf("MountDocs: %v", err)
	}
	for path, contentType := range map[string]string{
		"/openapi.json": "application/json; charset=utf-8",
		"/openapi.yaml": "application/yaml; charset=utf-8",
		"/docs":         "text/html; charset=utf-8",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != contentType {
			t.Fatalf("%s = status %d type %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}
