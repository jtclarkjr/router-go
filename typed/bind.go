package typed

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

func bindRequest[I any](w http.ResponseWriter, req *http.Request, plan bindingPlan, requestContentType string, maxBodyBytes int64, disallowUnknownFields bool) (I, error) {
	var zero I
	inputType := reflect.TypeFor[I]()
	if inputType == nil || inputType == reflect.TypeFor[Empty]() {
		return zero, nil
	}

	input := reflect.New(inputType).Elem()
	if plan.directBody {
		if err := decodeJSONBody(w, req, input, true, requestContentType, maxBodyBytes, disallowUnknownFields); err != nil {
			return zero, err
		}
		if err := validateInput(input, "body", "body"); err != nil {
			return zero, err
		}
		if err := customValidate(req, input); err != nil {
			return zero, customValidationError(err)
		}
		return input.Interface().(I), nil
	}

	ensurePointer(input)
	structValue := indirectValue(input)
	for _, binding := range plan.fields {
		field := structValue.FieldByIndex(binding.index)
		values, present := requestValues(req, binding)
		if !present {
			if binding.required {
				return zero, requestViolation(http.StatusBadRequest, "validation_error", binding, "required", "field is required", nil)
			}
			continue
		}
		if err := setFromStrings(field, values); err != nil {
			return zero, requestViolation(http.StatusBadRequest, "binding_error", binding, "format", "field has an invalid format", err)
		}
		if violation := validateField(field, binding.field.Tag.Get("validate")); violation != nil {
			violation.Field = binding.name
			violation.In = binding.in
			return zero, &RequestError{Code: "validation_error", Message: "request validation failed", Violations: []FieldViolation{*violation}}
		}
	}

	if plan.body != nil {
		bodyField := structValue.FieldByIndex(plan.body.index)
		if err := decodeJSONBody(w, req, bodyField, plan.body.required, requestContentType, maxBodyBytes, disallowUnknownFields); err != nil {
			return zero, err
		}
		if err := validateInput(bodyField, plan.body.field.Name, "body"); err != nil {
			return zero, err
		}
		if err := customValidate(req, bodyField); err != nil {
			return zero, customValidationError(err)
		}
	}
	if err := customValidate(req, input); err != nil {
		return zero, customValidationError(err)
	}
	return input.Interface().(I), nil
}

func requestValues(req *http.Request, binding fieldBinding) ([]string, bool) {
	switch binding.in {
	case "path":
		value := req.PathValue(binding.name)
		if value != "" && parameterIsSlice(binding.field.Type) {
			return strings.Split(value, ","), true
		}
		return []string{value}, value != ""
	case "query":
		values, exists := req.URL.Query()[binding.name]
		return values, exists
	case "header":
		values := req.Header.Values(binding.name)
		if len(values) == 0 {
			return nil, false
		}
		if parameterIsSlice(binding.field.Type) {
			result := make([]string, 0, len(values))
			for _, value := range values {
				result = append(result, strings.Split(value, ",")...)
			}
			return result, true
		}
		return values, true
	case "cookie":
		if parameterIsSlice(binding.field.Type) {
			var values []string
			for _, cookie := range req.Cookies() {
				if cookie.Name == binding.name {
					values = append(values, cookie.Value)
				}
			}
			return values, len(values) > 0
		}
		cookie, err := req.Cookie(binding.name)
		if err != nil {
			return nil, false
		}
		return []string{cookie.Value}, true
	default:
		return nil, false
	}
}

func parameterIsSlice(valueType reflect.Type) bool {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType.Kind() == reflect.Slice
}

func setFromStrings(destination reflect.Value, values []string) error {
	if !destination.CanSet() {
		return errors.New("destination cannot be set")
	}
	if destination.Kind() == reflect.Pointer {
		if destination.IsNil() {
			destination.Set(reflect.New(destination.Type().Elem()))
		}
		return setFromStrings(destination.Elem(), values)
	}
	if destination.Kind() == reflect.Slice {
		result := reflect.MakeSlice(destination.Type(), 0, len(values))
		for _, value := range values {
			element := reflect.New(destination.Type().Elem()).Elem()
			if err := setFromStrings(element, []string{value}); err != nil {
				return err
			}
			result = reflect.Append(result, element)
		}
		destination.Set(result)
		return nil
	}
	if len(values) == 0 {
		return errors.New("no values")
	}

	if destination.CanAddr() && destination.Addr().Type().Implements(textUnmarshalerType) {
		return destination.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(values[0]))
	}
	value := values[0]
	switch destination.Kind() {
	case reflect.String:
		destination.SetString(value)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		destination.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, destination.Type().Bits())
		if err != nil {
			return err
		}
		destination.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(value, 10, destination.Type().Bits())
		if err != nil {
			return err
		}
		destination.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, destination.Type().Bits())
		if err != nil {
			return err
		}
		destination.SetFloat(parsed)
	default:
		return fmt.Errorf("unsupported parameter type %s", destination.Type())
	}
	return nil
}

func decodeJSONBody(w http.ResponseWriter, req *http.Request, destination reflect.Value, required bool, expectedContentType string, maxBodyBytes int64, disallowUnknownFields bool) error {
	if req.Body == nil || req.Body == http.NoBody {
		if required {
			return &RequestError{Code: "body_required", Message: "request body is required"}
		}
		return nil
	}
	if contentType := req.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != expectedContentType {
			return &RequestError{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media_type", Message: "Content-Type must be " + expectedContentType, Cause: err}
		}
	}
	if maxBodyBytes > 0 {
		req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	}

	if !destination.CanAddr() {
		return &RequestError{Code: "binding_error", Message: "request body destination is not addressable"}
	}
	decodeTarget := destination.Addr()
	decoder := json.NewDecoder(req.Body)
	if disallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(decodeTarget.Interface()); err != nil {
		if errors.Is(err, io.EOF) {
			if !required {
				return nil
			}
			return &RequestError{Code: "body_required", Message: "request body is required", Cause: err}
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return &RequestError{Status: http.StatusRequestEntityTooLarge, Code: "body_too_large", Message: "request body exceeds the configured limit", Cause: err}
		}
		return &RequestError{Code: "invalid_json", Message: "request body contains invalid JSON", Cause: err}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return &RequestError{Status: http.StatusRequestEntityTooLarge, Code: "body_too_large", Message: "request body exceeds the configured limit", Cause: err}
		}
		return &RequestError{Code: "invalid_json", Message: "request body must contain a single JSON value", Cause: err}
	}
	return nil
}

func ensurePointer(value reflect.Value) {
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() && value.CanSet() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		value = value.Elem()
	}
}

func indirectValue(value reflect.Value) reflect.Value {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	return value
}

func requestViolation(status int, code string, binding fieldBinding, rule, message string, cause error) error {
	return &RequestError{
		Status:  status,
		Code:    code,
		Message: "request validation failed",
		Violations: []FieldViolation{{
			Field:   binding.name,
			In:      binding.in,
			Rule:    rule,
			Message: message,
		}},
		Cause: cause,
	}
}

func customValidate(req *http.Request, value reflect.Value) error {
	if !value.IsValid() || !value.CanInterface() {
		return nil
	}
	for value.Kind() == reflect.Pointer && !value.IsNil() {
		if validator, ok := value.Interface().(interface{ Validate(*http.Request) error }); ok {
			return validator.Validate(req)
		}
		if validator, ok := value.Interface().(interface{ Validate() error }); ok {
			return validator.Validate()
		}
		value = value.Elem()
	}
	if value.CanAddr() {
		if validator, ok := value.Addr().Interface().(interface{ Validate(*http.Request) error }); ok {
			return validator.Validate(req)
		}
		if validator, ok := value.Addr().Interface().(interface{ Validate() error }); ok {
			return validator.Validate()
		}
	}
	return nil
}

func customValidationError(err error) error {
	return &RequestError{
		Code:    "validation_error",
		Message: "request validation failed",
		Violations: []FieldViolation{{
			Field:   "request",
			In:      "request",
			Rule:    "custom",
			Message: err.Error(),
		}},
		Cause: err,
	}
}

func normalizedFieldName(field reflect.StructField) string {
	if name := strings.Split(field.Tag.Get("json"), ",")[0]; name != "" && name != "-" {
		return name
	}
	return field.Name
}
