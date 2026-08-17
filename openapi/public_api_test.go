package openapi_test

import (
	"net/http"
	"reflect"

	"github.com/jtclarkjr/router-go/openapi"
)

var (
	_ func(openapi.Info) *openapi.Registry                             = openapi.New
	_ func(*openapi.Registry, string, string, openapi.Operation) error = (*openapi.Registry).Add
	_ func(*openapi.Registry, string, string, openapi.Operation)       = (*openapi.Registry).MustAdd
	_ func(*openapi.Registry, string, string)                          = (*openapi.Registry).Remove
	_ func(*openapi.Registry, reflect.Type) *openapi.Schema            = (*openapi.Registry).SchemaForType
	_ func(*openapi.Registry, reflect.Type, string) *openapi.Schema    = (*openapi.Registry).SchemaForTypeWithValidation
	_ func(*openapi.Registry, bool)                                    = (*openapi.Registry).SetStructAdditionalPropertiesAllowed
	_ func(*openapi.Registry, string, openapi.SecurityScheme) error    = (*openapi.Registry).AddSecurityScheme
	_ func(*openapi.Registry) openapi.Document                         = (*openapi.Registry).Document
	_ func(*openapi.Registry) ([]byte, error)                          = (*openapi.Registry).JSON
	_ func(*openapi.Registry) ([]byte, error)                          = (*openapi.Registry).YAML
	_ func(*openapi.Registry) http.Handler                             = (*openapi.Registry).JSONHandler
	_ func(*openapi.Registry) http.Handler                             = (*openapi.Registry).YAMLHandler
	_ func(string, string) http.Handler                                = openapi.SwaggerUI
	_                                                                  = openapi.SchemaFor[string]
	_                                                                  = openapi.Operation{}
	_                                                                  = openapi.Schema{}
)
