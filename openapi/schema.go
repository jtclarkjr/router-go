package openapi

import (
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	timeType            = reflect.TypeFor[time.Time]()
	rawMessageType      = reflect.TypeFor[json.RawMessage]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

func (r *Registry) schemaForTypeLocked(t reflect.Type, visiting map[reflect.Type]bool) *Schema {
	if t == nil {
		return &Schema{}
	}
	if t.Kind() == reflect.Pointer {
		valueSchema := r.schemaForTypeLocked(t.Elem(), visiting)
		return &Schema{OneOf: []*Schema{valueSchema, {Type: "null"}}}
	}
	if t == timeType {
		return &Schema{Type: "string", Format: "date-time"}
	}
	if t == rawMessageType {
		return &Schema{}
	}
	if reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return &Schema{Type: "string"}
	}

	switch t.Kind() {
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return &Schema{Type: "integer", Format: "int32"}
	case reflect.Int64:
		return &Schema{Type: "integer", Format: "int64"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &Schema{Type: "integer", Format: "int32", Minimum: floatPointer(0)}
	case reflect.Uint64:
		return &Schema{Type: "integer", Format: "int64", Minimum: floatPointer(0)}
	case reflect.Float32:
		return &Schema{Type: "number", Format: "float"}
	case reflect.Float64:
		return &Schema{Type: "number", Format: "double"}
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Slice, reflect.Array:
		// encoding/json uses base64 only for byte slices. Byte arrays remain
		// ordinary JSON arrays and must not be advertised as strings.
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return &Schema{Type: "string", Format: "byte"}
		}
		return &Schema{Type: "array", Items: r.schemaForTypeLocked(t.Elem(), visiting)}
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return &Schema{Type: "object"}
		}
		return &Schema{Type: "object", AdditionalProperties: r.schemaForTypeLocked(t.Elem(), visiting)}
	case reflect.Interface:
		return &Schema{}
	case reflect.Struct:
		return r.structSchemaLocked(t, visiting)
	default:
		return &Schema{}
	}
}

func (r *Registry) structSchemaLocked(t reflect.Type, visiting map[reflect.Type]bool) *Schema {
	if t.Name() != "" {
		if name, exists := r.typeNames[t]; exists {
			return &Schema{Ref: "#/components/schemas/" + name}
		}

		name := r.availableSchemaNameLocked(t)
		r.typeNames[t] = name
		r.document.Components.Schemas[name] = &Schema{Type: "object"}
		if visiting[t] {
			return &Schema{Ref: "#/components/schemas/" + name}
		}
		visiting[t] = true
		r.document.Components.Schemas[name] = r.inlineStructSchemaLocked(t, visiting)
		delete(visiting, t)
		return &Schema{Ref: "#/components/schemas/" + name}
	}
	return r.inlineStructSchemaLocked(t, visiting)
}

func (r *Registry) availableSchemaNameLocked(t reflect.Type) string {
	packageName := t.PkgPath()
	if index := strings.LastIndex(packageName, "/"); index >= 0 {
		packageName = packageName[index+1:]
	}
	readable := sanitizeComponentName(packageName + "_" + t.Name())
	// PkgPath plus String is not sufficient for named types declared inside
	// functions: two distinct local types can both report (for example)
	// "example.test/api.Payload" while having different fields. Include a
	// structural identity so those types cannot overwrite one another, while
	// preserving deterministic names across process and registration order.
	identity := schemaTypeIdentity(t)
	digest := sha256.Sum256([]byte(identity))
	return readable + "_" + hex.EncodeToString(digest[:8])
}

func schemaTypeIdentity(t reflect.Type) string {
	var identity strings.Builder
	appendSchemaTypeIdentity(&identity, t, make(map[reflect.Type]bool))
	return identity.String()
}

func appendSchemaTypeIdentity(identity *strings.Builder, t reflect.Type, visiting map[reflect.Type]bool) {
	if t == nil {
		writeIdentityPart(identity, "nil")
		return
	}
	writeIdentityPart(identity, t.Kind().String())
	writeIdentityPart(identity, t.PkgPath())
	writeIdentityPart(identity, t.String())
	if visiting[t] {
		writeIdentityPart(identity, "recursive")
		return
	}
	visiting[t] = true
	defer delete(visiting, t)

	switch t.Kind() {
	case reflect.Pointer, reflect.Slice:
		appendSchemaTypeIdentity(identity, t.Elem(), visiting)
	case reflect.Array:
		writeIdentityPart(identity, strconv.Itoa(t.Len()))
		appendSchemaTypeIdentity(identity, t.Elem(), visiting)
	case reflect.Map:
		appendSchemaTypeIdentity(identity, t.Key(), visiting)
		appendSchemaTypeIdentity(identity, t.Elem(), visiting)
	case reflect.Struct:
		writeIdentityPart(identity, strconv.Itoa(t.NumField()))
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			writeIdentityPart(identity, field.Name)
			writeIdentityPart(identity, field.PkgPath)
			writeIdentityPart(identity, strconv.FormatBool(field.Anonymous))
			writeIdentityPart(identity, string(field.Tag))
			appendSchemaTypeIdentity(identity, field.Type, visiting)
		}
	}
}

func writeIdentityPart(identity *strings.Builder, value string) {
	identity.WriteString(strconv.Itoa(len(value)))
	identity.WriteByte(':')
	identity.WriteString(value)
	identity.WriteByte(';')
}

func sanitizeComponentName(value string) string {
	var result strings.Builder
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '.', character == '-', character == '_':
			result.WriteRune(character)
		default:
			result.WriteByte('_')
		}
	}
	if result.Len() == 0 {
		return "schema"
	}
	return result.String()
}

func (r *Registry) inlineStructSchemaLocked(t reflect.Type, visiting map[reflect.Type]bool) *Schema {
	schema := &Schema{
		Type:                        "object",
		Properties:                  make(map[string]*Schema),
		AdditionalPropertiesAllowed: boolPointer(r.structAdditionalPropertiesAllowed),
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			embeddedType := field.Type
			for embeddedType.Kind() == reflect.Pointer {
				embeddedType = embeddedType.Elem()
			}
			if !field.Anonymous || embeddedType.Kind() != reflect.Struct {
				continue
			}
		}

		jsonName, _, skip := jsonFieldName(field)
		if skip {
			continue
		}
		if field.Anonymous && field.Tag.Get("json") == "" {
			embeddedType := field.Type
			for embeddedType.Kind() == reflect.Pointer {
				embeddedType = embeddedType.Elem()
			}
			embedded := r.schemaForTypeLocked(embeddedType, visiting)
			if embedded.Ref != "" {
				name := strings.TrimPrefix(embedded.Ref, "#/components/schemas/")
				embedded = r.document.Components.Schemas[name]
			}
			if embedded != nil {
				for propertyName, property := range embedded.Properties {
					schema.Properties[propertyName] = property
				}
				schema.Required = append(schema.Required, embedded.Required...)
			}
			continue
		}

		property := r.schemaForTypeLocked(field.Type, visiting)
		property.Description = field.Tag.Get("description")
		applyValidationSchema(property, field.Tag.Get("validate"), field.Type)
		propertyType := validationSchemaTarget(property).Type
		if example := field.Tag.Get("example"); example != "" {
			property.Example = schemaEnumValue(propertyType, example)
		}
		if defaultValue := field.Tag.Get("default"); defaultValue != "" {
			property.Default = schemaEnumValue(propertyType, defaultValue)
		}
		schema.Properties[jsonName] = property
		if hasValidation(field.Tag.Get("validate"), "required") && !contains(schema.Required, jsonName) {
			schema.Required = append(schema.Required, jsonName)
		}
	}
	if len(schema.Properties) == 0 {
		schema.Properties = nil
	}
	return schema
}

func jsonFieldName(field reflect.StructField) (string, bool, bool) {
	tag := field.Tag.Get("json")
	parts := strings.Split(tag, ",")
	if parts[0] == "-" {
		return "", false, true
	}
	name := parts[0]
	if name == "" {
		name = field.Name
	}
	omitEmpty := false
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitEmpty = true
		}
	}
	return name, omitEmpty, false
}

func applyValidationSchema(schema *Schema, tag string, valueType reflect.Type) {
	if schema == nil || tag == "" {
		return
	}
	target := validationSchemaTarget(schema)
	for _, rule := range strings.Split(tag, ",") {
		name, value, hasValue := strings.Cut(rule, "=")
		if name == "required" {
			applyRequiredSchema(schema, presenceOnlyRequired(valueType))
			continue
		}
		if !hasValue {
			continue
		}
		switch name {
		case "min":
			if number, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
				applySchemaMinimum(target, number)
			}
		case "max":
			if number, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
				applySchemaMaximum(target, number)
			}
		case "len":
			if number, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
				if isSchemaLengthType(target.Type) && (number < 0 || number != math.Trunc(number)) {
					target.Not = &Schema{}
				} else {
					applySchemaMinimum(target, number)
					applySchemaMaximum(target, number)
				}
			}
		case "oneof":
			values := strings.Fields(value)
			target.Enum = make([]any, len(values))
			for i := range values {
				target.Enum[i] = schemaEnumValue(target.Type, values[i])
			}
		case "regexp":
			target.Pattern = value
		}
	}
}

func applyRequiredSchema(schema *Schema, presenceOnly bool) {
	if schema == nil {
		return
	}
	if len(schema.OneOf) > 0 {
		nonNull := make([]*Schema, 0, len(schema.OneOf))
		for _, candidate := range schema.OneOf {
			if candidate != nil && candidate.Type == "null" {
				continue
			}
			nonNull = append(nonNull, candidate)
		}
		schema.OneOf = nonNull
	}
	if presenceOnly {
		return
	}
	target := validationSchemaTarget(schema)
	switch target.Type {
	case "string":
		if target.MinLength == nil || *target.MinLength < 1 {
			target.MinLength = intPointer(1)
		}
	case "array":
		if target.MinItems == nil || *target.MinItems < 1 {
			target.MinItems = intPointer(1)
		}
	case "object":
		if target.MinProperties == nil || *target.MinProperties < 1 {
			target.MinProperties = intPointer(1)
		}
	case "boolean":
		target.Const = true
	case "integer", "number":
		target.Not = &Schema{Const: 0}
	}
}

func presenceOnlyRequired(valueType reflect.Type) bool {
	return valueType != nil &&
		(valueType.Kind() == reflect.Pointer || valueType.Kind() == reflect.Interface)
}

func validationSchemaTarget(schema *Schema) *Schema {
	if schema.Type != "" || len(schema.OneOf) == 0 {
		return schema
	}
	for _, candidate := range schema.OneOf {
		if candidate != nil && candidate.Type != "null" {
			return candidate
		}
	}
	return schema
}

func applySchemaMinimum(schema *Schema, value float64) {
	switch schema.Type {
	case "string":
		if value < 0 {
			return
		}
		length := int(math.Ceil(value))
		schema.MinLength = &length
	case "array":
		if value < 0 {
			return
		}
		length := int(math.Ceil(value))
		schema.MinItems = &length
	case "object":
		if value < 0 {
			return
		}
		length := int(math.Ceil(value))
		schema.MinProperties = &length
	default:
		schema.Minimum = &value
	}
}

func applySchemaMaximum(schema *Schema, value float64) {
	switch schema.Type {
	case "string":
		if value < 0 {
			schema.Not = &Schema{}
			return
		}
		length := int(math.Floor(value))
		schema.MaxLength = &length
	case "array":
		if value < 0 {
			schema.Not = &Schema{}
			return
		}
		length := int(math.Floor(value))
		schema.MaxItems = &length
	case "object":
		if value < 0 {
			schema.Not = &Schema{}
			return
		}
		length := int(math.Floor(value))
		schema.MaxProperties = &length
	default:
		schema.Maximum = &value
	}
}

func isSchemaLengthType(schemaType string) bool {
	return schemaType == "string" || schemaType == "array" || schemaType == "object"
}

func schemaEnumValue(schemaType, value string) any {
	switch schemaType {
	case "integer":
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
		if _, err := strconv.ParseUint(value, 10, 64); err == nil {
			return json.Number(value)
		}
	case "number":
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
			return parsed
		}
	case "boolean":
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return value
}

func hasValidation(tag, wanted string) bool {
	for _, rule := range strings.Split(tag, ",") {
		name, _, _ := strings.Cut(rule, "=")
		if name == wanted {
			return true
		}
	}
	return false
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func floatPointer(value float64) *float64 { return &value }

func boolPointer(value bool) *bool { return &value }

func intPointer(value int) *int { return &value }
