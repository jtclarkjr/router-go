package openapi

import (
	"encoding/json"
	"strings"
)

// Info identifies an OpenAPI document.
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// Server describes an API server.
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Tag describes an operation tag.
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Document is an OpenAPI 3.1 document.
type Document struct {
	OpenAPI           string                          `json:"openapi"`
	JSONSchemaDialect string                          `json:"jsonSchemaDialect,omitempty"`
	Info              Info                            `json:"info"`
	Servers           []Server                        `json:"servers,omitempty"`
	Tags              []Tag                           `json:"tags,omitempty"`
	Paths             map[string]map[string]Operation `json:"paths"`
	Components        Components                      `json:"components,omitempty"`
}

// Components contains reusable OpenAPI components.
type Components struct {
	Schemas         map[string]*Schema        `json:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

// SecurityScheme defines an OpenAPI authentication scheme.
type SecurityScheme struct {
	Type         string `json:"type"`
	Description  string `json:"description,omitempty"`
	Name         string `json:"name,omitempty"`
	In           string `json:"in,omitempty"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
}

// SecurityRequirement maps a security scheme to required scopes.
type SecurityRequirement map[string][]string

// Operation describes one HTTP operation.
type Operation struct {
	OperationID string                `json:"operationId,omitempty"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	Tags        []string              `json:"tags,omitempty"`
	Deprecated  bool                  `json:"deprecated,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses"`
	Security    []SecurityRequirement `json:"security,omitempty"`
	Extensions  map[string]any        `json:"-"`
}

// MarshalJSON includes extension keys (for example x-router-kind) alongside
// the standard Operation fields.
func (o Operation) MarshalJSON() ([]byte, error) {
	type operationAlias Operation
	base, err := json.Marshal(operationAlias(o))
	if err != nil {
		return nil, err
	}
	if len(o.Extensions) == 0 {
		return base, nil
	}
	var result map[string]any
	if err := decodeJSONNumber(base, &result); err != nil {
		return nil, err
	}
	for key, value := range o.Extensions {
		if strings.HasPrefix(key, "x-") {
			result[key] = value
		}
	}
	return json.Marshal(result)
}

// UnmarshalJSON preserves extension keys in document snapshots.
func (o *Operation) UnmarshalJSON(data []byte) error {
	type operationAlias Operation
	var decoded operationAlias
	if err := decodeJSONNumber(data, &decoded); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	decoded.Extensions = make(map[string]any)
	for key, value := range raw {
		if !strings.HasPrefix(key, "x-") {
			continue
		}
		var extension any
		if err := decodeJSONNumber(value, &extension); err != nil {
			return err
		}
		decoded.Extensions[key] = extension
	}
	if len(decoded.Extensions) == 0 {
		decoded.Extensions = nil
	}
	*o = Operation(decoded)
	return nil
}

// Parameter describes a path, query, header, or cookie value.
type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Deprecated  bool    `json:"deprecated,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

// RequestBody describes an HTTP request body.
type RequestBody struct {
	Description string               `json:"description,omitempty"`
	Required    bool                 `json:"required,omitempty"`
	Content     map[string]MediaType `json:"content"`
}

// Response describes an HTTP response.
type Response struct {
	Description string               `json:"description"`
	Headers     map[string]Header    `json:"headers,omitempty"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// Header describes a response header.
type Header struct {
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

// MediaType associates a schema and optional example with a content type.
type MediaType struct {
	Schema  *Schema `json:"schema,omitempty"`
	Example any     `json:"example,omitempty"`
}

// Schema is the JSON Schema vocabulary used by OpenAPI 3.1.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Title                string             `json:"title,omitempty"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	// AdditionalPropertiesAllowed emits a boolean additionalProperties value.
	// It is separate from AdditionalProperties so existing callers can keep
	// supplying a schema for map values.
	AdditionalPropertiesAllowed *bool     `json:"-"`
	Enum                        []any     `json:"enum,omitempty"`
	Const                       any       `json:"const,omitempty"`
	OneOf                       []*Schema `json:"oneOf,omitempty"`
	Not                         *Schema   `json:"not,omitempty"`
	Minimum                     *float64  `json:"minimum,omitempty"`
	Maximum                     *float64  `json:"maximum,omitempty"`
	MinLength                   *int      `json:"minLength,omitempty"`
	MaxLength                   *int      `json:"maxLength,omitempty"`
	MinItems                    *int      `json:"minItems,omitempty"`
	MaxItems                    *int      `json:"maxItems,omitempty"`
	MinProperties               *int      `json:"minProperties,omitempty"`
	MaxProperties               *int      `json:"maxProperties,omitempty"`
	Pattern                     string    `json:"pattern,omitempty"`
	Default                     any       `json:"default,omitempty"`
	Example                     any       `json:"example,omitempty"`
}

// MarshalJSON supports both schema-valued and boolean additionalProperties.
func (s Schema) MarshalJSON() ([]byte, error) {
	type schemaAlias Schema
	encoded, err := json.Marshal(schemaAlias(s))
	if err != nil || s.AdditionalPropertiesAllowed == nil {
		return encoded, err
	}
	var result map[string]any
	if err := decodeJSONNumber(encoded, &result); err != nil {
		return nil, err
	}
	result["additionalProperties"] = *s.AdditionalPropertiesAllowed
	return json.Marshal(result)
}

// UnmarshalJSON preserves boolean additionalProperties in document snapshots.
func (s *Schema) UnmarshalJSON(data []byte) error {
	type schemaAlias Schema
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	additionalProperties, hasAdditionalProperties := raw["additionalProperties"]
	if hasAdditionalProperties {
		var allowed bool
		if err := json.Unmarshal(additionalProperties, &allowed); err == nil {
			delete(raw, "additionalProperties")
			withoutBoolean, err := json.Marshal(raw)
			if err != nil {
				return err
			}
			var decoded schemaAlias
			if err := decodeJSONNumber(withoutBoolean, &decoded); err != nil {
				return err
			}
			decoded.AdditionalPropertiesAllowed = boolPointer(allowed)
			*s = Schema(decoded)
			return nil
		}
	}
	var decoded schemaAlias
	if err := decodeJSONNumber(data, &decoded); err != nil {
		return err
	}
	*s = Schema(decoded)
	return nil
}
