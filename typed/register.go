package typed

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	router "github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/openapi"
)

// Register adds a typed handler to both the HTTP router and OpenAPI registry.
// If HTTP registration fails, the documented operation is rolled back.
func Register[I, O any](r *Router, operation Operation[I, O], handler Handler[I, O]) error {
	return RegisterWithMiddleware(r, operation, handler)
}

// RegisterWithMiddleware adds a typed handler with operation-specific
// middleware. The middleware runs before request binding and validation and
// inside any middleware installed through Router.Use.
func RegisterWithMiddleware[I, O any](r *Router, operation Operation[I, O], handler Handler[I, O], middleware ...router.Middleware) error {
	if r == nil || r.base == nil || r.registry == nil {
		return errors.New("typed: nil router")
	}
	if handler == nil {
		return errors.New("typed: nil handler")
	}

	plan, err := buildBindingPlan(reflect.TypeFor[I](), operation.Path)
	if err != nil {
		return fmt.Errorf("typed: %s %s input: %w", operation.Method, operation.Path, err)
	}
	requestContentType, err := jsonMediaType(operation.RequestContentType, "application/json")
	if err != nil {
		return fmt.Errorf("typed: request content type: %w", err)
	}
	if _, err := jsonMediaType(operation.ResponseContentType, "application/json"); err != nil {
		return fmt.Errorf("typed: response content type: %w", err)
	}
	spec, successStatus, err := buildOperationSpec[I, O](r.registry, operation, plan)
	if err != nil {
		return err
	}
	if err := r.registry.Add(operation.Path, operation.Method, spec); err != nil {
		return err
	}
	allowedStatuses := map[int]bool{successStatus: true}
	for status := range operation.AdditionalResponses {
		allowedStatuses[status] = true
	}

	var httpHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		input, bindErr := bindRequest[I](w, req, plan, requestContentType, r.maxBodyBytes, r.disallowUnknownFields)
		if bindErr != nil {
			r.errorCodec.WriteError(w, req, bindErr)
			return
		}

		response, handlerErr := handler(req, input)
		if handlerErr != nil {
			r.errorCodec.WriteError(w, req, handlerErr)
			return
		}
		if response.Status == 0 {
			response.Status = successStatus
		}
		if !allowedStatuses[response.Status] {
			r.errorCodec.WriteError(w, req, &Error{
				Status:  http.StatusInternalServerError,
				Code:    "undocumented_response_status",
				Message: fmt.Sprintf("handler returned undocumented status %d", response.Status),
			})
			return
		}
		committed, err := writeResponse(w, req, response, operation.ResponseContentType)
		if err != nil && !committed {
			r.errorCodec.WriteError(w, req, err)
		}
	})
	httpHandler = applyMiddleware(httpHandler, middleware)

	if err := r.base.Register(operation.Method, operation.Path, httpHandler); err != nil {
		r.registry.Remove(operation.Path, operation.Method)
		return err
	}
	return nil
}

// MustRegister registers a typed operation and panics on failure.
func MustRegister[I, O any](r *Router, operation Operation[I, O], handler Handler[I, O]) {
	if err := Register(r, operation, handler); err != nil {
		panic(err)
	}
}

// MustRegisterWithMiddleware registers a typed operation with middleware and
// panics on failure.
func MustRegisterWithMiddleware[I, O any](r *Router, operation Operation[I, O], handler Handler[I, O], middleware ...router.Middleware) {
	if err := RegisterWithMiddleware(r, operation, handler, middleware...); err != nil {
		panic(err)
	}
}

// RegisterRaw registers a documented net/http handler without typed body
// buffering. Use it for SSE, multipart, reverse proxies, and WebSockets.
func RegisterRaw(r *Router, operation RawOperation, handler http.Handler) error {
	return RegisterRawWithMiddleware(r, operation, handler)
}

// RegisterRawWithMiddleware registers a documented raw handler with
// operation-specific middleware. The middleware runs inside any middleware
// installed through Router.Use.
func RegisterRawWithMiddleware(r *Router, operation RawOperation, handler http.Handler, middleware ...router.Middleware) error {
	if r == nil || r.base == nil || r.registry == nil {
		return errors.New("typed: nil router")
	}
	if handler == nil {
		return errors.New("typed: nil raw handler")
	}
	if operation.Kind == "" {
		operation.Kind = RawHTTP
	}
	if operation.Spec.Extensions == nil {
		operation.Spec.Extensions = make(map[string]any)
	}
	operation.Spec.Extensions["x-router-kind"] = string(operation.Kind)
	documentPath := operation.DocumentPath
	if documentPath == "" {
		documentPath = operation.Path
	}
	if documentPath != operation.Path {
		operation.Spec.Extensions["x-router-runtime-path"] = operation.Path
	}
	if err := r.registry.Add(documentPath, operation.Method, operation.Spec); err != nil {
		return err
	}
	if err := r.base.Register(operation.Method, operation.Path, applyMiddleware(handler, middleware)); err != nil {
		r.registry.Remove(documentPath, operation.Method)
		return err
	}
	return nil
}

func applyMiddleware(handler http.Handler, middleware []router.Middleware) http.Handler {
	for index := len(middleware) - 1; index >= 0; index-- {
		if middleware[index] != nil {
			handler = middleware[index](handler)
		}
	}
	return handler
}

// MustRegisterRaw registers a raw operation and panics on failure.
func MustRegisterRaw(r *Router, operation RawOperation, handler http.Handler) {
	if err := RegisterRaw(r, operation, handler); err != nil {
		panic(err)
	}
}

// MustRegisterRawWithMiddleware registers a raw operation with middleware and
// panics on failure.
func MustRegisterRawWithMiddleware(r *Router, operation RawOperation, handler http.Handler, middleware ...router.Middleware) {
	if err := RegisterRawWithMiddleware(r, operation, handler, middleware...); err != nil {
		panic(err)
	}
}

func buildOperationSpec[I, O any](registry *openapi.Registry, operation Operation[I, O], plan bindingPlan) (openapi.Operation, int, error) {
	if operation.Method == "" || operation.Path == "" {
		return openapi.Operation{}, 0, errors.New("typed: operation method and path are required")
	}
	requestContentType, err := jsonMediaType(operation.RequestContentType, "application/json")
	if err != nil {
		return openapi.Operation{}, 0, err
	}
	responseContentType, err := jsonMediaType(operation.ResponseContentType, "application/json")
	if err != nil {
		return openapi.Operation{}, 0, err
	}
	outputType := reflect.TypeFor[O]()
	noBody := outputType == reflect.TypeFor[NoBody]()
	successStatus := operation.SuccessStatus
	if successStatus == 0 {
		if noBody {
			successStatus = http.StatusNoContent
		} else {
			successStatus = http.StatusOK
		}
	}
	if successStatus < 100 || successStatus > 599 {
		return openapi.Operation{}, 0, fmt.Errorf("typed: invalid success status %d", successStatus)
	}
	if _, exists := operation.AdditionalResponses[successStatus]; exists {
		return openapi.Operation{}, 0, fmt.Errorf("typed: additional response %d conflicts with the success response", successStatus)
	}
	for status := range operation.AdditionalResponses {
		if status < 100 || status > 599 {
			return openapi.Operation{}, 0, fmt.Errorf("typed: invalid additional response status %d", status)
		}
	}

	parameters := make([]openapi.Parameter, 0, len(plan.fields))
	for _, field := range plan.fields {
		parameter := openapi.Parameter{
			Name:        field.name,
			In:          field.in,
			Description: field.field.Tag.Get("description"),
			Required:    field.required || field.in == "path",
			Deprecated:  field.field.Tag.Get("deprecated") == "true",
			Schema:      registry.SchemaForTypeWithValidation(parameterSchemaType(field.field.Type), field.field.Tag.Get("validate")),
		}
		parameters = append(parameters, parameter)
	}

	var requestBody *openapi.RequestBody
	if plan.directBody {
		requestBody = &openapi.RequestBody{
			Required: true,
			Content: map[string]openapi.MediaType{
				requestContentType: {Schema: registry.SchemaForType(reflect.TypeFor[I]())},
			},
		}
	} else if plan.body != nil {
		requestBody = &openapi.RequestBody{
			Description: plan.body.field.Tag.Get("description"),
			Required:    plan.body.required,
			Content: map[string]openapi.MediaType{
				requestContentType: {Schema: registry.SchemaForType(plan.body.field.Type)},
			},
		}
	}

	successDescription := operation.SuccessDescription
	if successDescription == "" {
		successDescription = http.StatusText(successStatus)
		if successDescription == "" {
			successDescription = "Successful response"
		}
	}
	successResponse := openapi.Response{
		Description: successDescription,
		Headers:     operation.ResponseHeaders,
	}
	suppressesBody := noBody || successStatus == http.StatusNoContent || successStatus == http.StatusNotModified || strings.EqualFold(operation.Method, http.MethodHead)
	if !suppressesBody {
		successResponse.Content = map[string]openapi.MediaType{
			responseContentType: {Schema: registry.SchemaForType(outputType)},
		}
	}
	responses := map[string]openapi.Response{strconv.Itoa(successStatus): successResponse}
	for status, response := range operation.AdditionalResponses {
		responses[strconv.Itoa(status)] = response
	}

	return openapi.Operation{
		OperationID: operation.OperationID,
		Summary:     operation.Summary,
		Description: operation.Description,
		Tags:        append([]string(nil), operation.Tags...),
		Deprecated:  operation.Deprecated,
		Parameters:  parameters,
		RequestBody: requestBody,
		Responses:   responses,
		Security:    operation.Security,
	}, successStatus, nil
}

func writeResponse[O any](w http.ResponseWriter, req *http.Request, response Response[O], contentType string) (bool, error) {
	if contentType == "" {
		contentType = "application/json"
	}
	mediaType, err := jsonMediaType(contentType, "application/json")
	if err != nil {
		return false, &Error{Status: http.StatusInternalServerError, Code: "invalid_response_content_type", Message: "handler has an invalid response content type", Cause: err}
	}

	status := response.Status
	if status < 100 || status > 599 {
		return false, &Error{Status: http.StatusInternalServerError, Code: "invalid_response_status", Message: "handler returned an invalid response status"}
	}
	if status == http.StatusNoContent || status == http.StatusNotModified || strings.EqualFold(req.Method, http.MethodHead) || reflect.TypeFor[O]() == reflect.TypeFor[NoBody]() {
		copyResponseHeaders(w.Header(), response.Header)
		w.WriteHeader(status)
		return true, nil
	}
	payload, err := json.Marshal(response.Body)
	if err != nil {
		return false, &Error{Status: http.StatusInternalServerError, Code: "response_encoding_failed", Message: "failed to encode response", Cause: err}
	}
	copyResponseHeaders(w.Header(), response.Header)
	w.Header().Set("Content-Type", mime.FormatMediaType(mediaType, map[string]string{"charset": "utf-8"}))
	w.WriteHeader(status)
	_, err = w.Write(payload)
	return true, err
}

func copyResponseHeaders(destination, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func jsonMediaType(value, fallback string) (string, error) {
	if value == "" {
		value = fallback
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return "", err
	}
	if mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") {
		return "", fmt.Errorf("%s is not a JSON media type; use RegisterRaw for another codec", mediaType)
	}
	return mediaType, nil
}

type bindingPlan struct {
	directBody bool
	fields     []fieldBinding
	body       *fieldBinding
}

type fieldBinding struct {
	index    []int
	name     string
	in       string
	required bool
	field    reflect.StructField
}

func buildBindingPlan(inputType reflect.Type, path string) (bindingPlan, error) {
	if inputType == nil || inputType == reflect.TypeFor[Empty]() {
		return bindingPlan{}, nil
	}
	if err := validateTypeRules(inputType, make(map[reflect.Type]bool)); err != nil {
		return bindingPlan{}, err
	}
	originalType := inputType
	for inputType.Kind() == reflect.Pointer {
		inputType = inputType.Elem()
	}
	if inputType.Kind() != reflect.Struct {
		return bindingPlan{directBody: true}, nil
	}

	hasTransportTag := false
	for i := 0; i < inputType.NumField(); i++ {
		field := inputType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if transportTagCount(field) > 0 {
			hasTransportTag = true
			break
		}
	}
	if !hasTransportTag && originalType != reflect.TypeFor[Empty]() {
		return bindingPlan{directBody: true}, nil
	}

	plan := bindingPlan{}
	pathParameters := extractPathParameters(path)
	boundPathParameters := make(map[string]bool)
	for i := 0; i < inputType.NumField(); i++ {
		field := inputType.Field(i)
		if field.PkgPath != "" || field.Tag.Get("transport") == "-" {
			continue
		}
		count := transportTagCount(field)
		if count == 0 {
			return bindingPlan{}, fmt.Errorf("exported field %s needs path, query, header, cookie, body, or transport:\"-\"", field.Name)
		}
		if count > 1 {
			return bindingPlan{}, fmt.Errorf("field %s has multiple transport tags", field.Name)
		}

		binding := fieldBinding{
			index:    field.Index,
			required: hasRule(field.Tag.Get("validate"), "required"),
			field:    field,
		}
		for _, location := range []string{"path", "query", "header", "cookie", "body"} {
			if name, exists := field.Tag.Lookup(location); exists {
				binding.in = location
				binding.name = name
				if binding.name == "" && location != "body" {
					binding.name = field.Name
				}
				break
			}
		}

		if binding.in == "body" {
			if binding.field.Type.Kind() != reflect.Pointer {
				binding.required = true
			}
			if plan.body != nil {
				return bindingPlan{}, errors.New("only one body field is allowed")
			}
			copy := binding
			plan.body = &copy
			continue
		}
		if binding.in == "path" {
			binding.required = true
			if !pathParameters[binding.name] {
				return bindingPlan{}, fmt.Errorf("path field %s does not exist in route %s", binding.name, path)
			}
			boundPathParameters[binding.name] = true
		}
		if err := validateParameterType(binding.field.Type); err != nil {
			return bindingPlan{}, fmt.Errorf("field %s: %w", field.Name, err)
		}
		plan.fields = append(plan.fields, binding)
	}
	for parameter := range pathParameters {
		if !boundPathParameters[parameter] {
			return bindingPlan{}, fmt.Errorf("route parameter %s has no typed path field", parameter)
		}
	}
	return plan, nil
}

func validateParameterType(valueType reflect.Type) error {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	if valueType.Kind() == reflect.Slice {
		valueType = valueType.Elem()
		for valueType.Kind() == reflect.Pointer {
			valueType = valueType.Elem()
		}
	}
	if reflect.PointerTo(valueType).Implements(textUnmarshalerType) {
		return nil
	}
	switch valueType.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return nil
	default:
		return fmt.Errorf("unsupported %s parameter type", valueType)
	}
}

func parameterSchemaType(valueType reflect.Type) reflect.Type {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType
}

func transportTagCount(field reflect.StructField) int {
	count := 0
	for _, name := range []string{"path", "query", "header", "cookie", "body"} {
		if _, exists := field.Tag.Lookup(name); exists {
			count++
		}
	}
	return count
}

func extractPathParameters(path string) map[string]bool {
	result := make(map[string]bool)
	for {
		start := strings.IndexByte(path, '{')
		if start < 0 {
			break
		}
		end := strings.IndexByte(path[start:], '}')
		if end < 0 {
			break
		}
		result[path[start+1:start+end]] = true
		path = path[start+end+1:]
	}
	return result
}
