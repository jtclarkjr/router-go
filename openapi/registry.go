package openapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"reflect"
	"strings"
	"sync"
)

var (
	// ErrDuplicateOperation indicates that an HTTP operation is already
	// documented for a path.
	ErrDuplicateOperation = errors.New("duplicate OpenAPI operation")
	// ErrInvalidOperation indicates invalid OpenAPI registration metadata.
	ErrInvalidOperation = errors.New("invalid OpenAPI operation")
)

// Registry owns a mutable, concurrency-safe OpenAPI document.
type Registry struct {
	mu                                sync.RWMutex
	document                          Document
	typeNames                         map[reflect.Type]string
	operationIDs                      map[string]string
	structAdditionalPropertiesAllowed bool
}

// SetStructAdditionalPropertiesAllowed controls whether subsequently
// generated Go struct schemas accept undeclared JSON properties. Existing
// generated struct components are updated as well.
func (r *Registry) SetStructAdditionalPropertiesAllowed(allowed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.structAdditionalPropertiesAllowed = allowed
	for valueType, name := range r.typeNames {
		if valueType.Kind() != reflect.Struct {
			continue
		}
		if schema := r.document.Components.Schemas[name]; schema != nil {
			schema.AdditionalPropertiesAllowed = boolPointer(allowed)
		}
	}
}

// New creates an OpenAPI 3.1 registry.
func New(info Info) *Registry {
	if info.Title == "" {
		info.Title = "API"
	}
	if info.Version == "" {
		info.Version = "0.0.0"
	}
	return &Registry{
		document: Document{
			OpenAPI:           "3.1.0",
			JSONSchemaDialect: "https://json-schema.org/draft/2020-12/schema",
			Info:              info,
			Paths:             make(map[string]map[string]Operation),
			Components: Components{
				Schemas:         make(map[string]*Schema),
				SecuritySchemes: make(map[string]SecurityScheme),
			},
		},
		typeNames:    make(map[reflect.Type]string),
		operationIDs: make(map[string]string),
	}
}

// Add registers an operation. Method names are normalized to lowercase.
func (r *Registry) Add(path, method string, operation Operation) error {
	method = strings.ToLower(strings.TrimSpace(method))
	if path == "" || !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: path must begin with /", ErrInvalidOperation)
	}
	if strings.ContainsAny(path, "?#") {
		return fmt.Errorf("%w: path must not contain a query or fragment", ErrInvalidOperation)
	}
	if strings.Contains(path, "*") {
		return fmt.Errorf("%w: wildcards must use an OpenAPI path parameter", ErrInvalidOperation)
	}
	if err := validatePathParameters(path, operation.Parameters); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidOperation, err)
	}
	if !supportedMethod(method) {
		return fmt.Errorf("%w: unsupported method %q", ErrInvalidOperation, method)
	}
	if err := validateOperation(operation); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidOperation, err)
	}
	operationCopy, err := cloneOperation(operation)
	if err != nil {
		return fmt.Errorf("%w: operation cannot be serialized: %v", ErrInvalidOperation, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.document.Paths[path][method]; exists {
		return fmt.Errorf("%w: %s %s", ErrDuplicateOperation, strings.ToUpper(method), path)
	}
	operationOwner := strings.ToUpper(method) + " " + path
	if operation.OperationID != "" {
		if owner, exists := r.operationIDs[operation.OperationID]; exists {
			return fmt.Errorf("%w: operationId %q is already used by %s", ErrDuplicateOperation, operation.OperationID, owner)
		}
	}
	if err := r.validateSecurityRequirementsLocked(operation.Security); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidOperation, err)
	}
	if r.document.Paths[path] == nil {
		r.document.Paths[path] = make(map[string]Operation)
	}
	r.document.Paths[path][method] = operationCopy
	if operation.OperationID != "" {
		r.operationIDs[operation.OperationID] = operationOwner
	}
	return nil
}

func validateOperation(operation Operation) error {
	if len(operation.Responses) == 0 {
		return errors.New("at least one response is required")
	}
	for status, response := range operation.Responses {
		if !validResponseStatus(status) {
			return fmt.Errorf("invalid response status %q", status)
		}
		if strings.TrimSpace(response.Description) == "" {
			return fmt.Errorf("response %s needs a description", status)
		}
	}
	seenParameters := make(map[string]bool)
	for _, parameter := range operation.Parameters {
		if parameter.Name == "" {
			return errors.New("parameter name is required")
		}
		switch parameter.In {
		case "path", "query", "header", "cookie":
		default:
			return fmt.Errorf("parameter %q has invalid location %q", parameter.Name, parameter.In)
		}
		if parameter.Schema == nil {
			return fmt.Errorf("parameter %q in %s needs a schema", parameter.Name, parameter.In)
		}
		parameterName := parameter.Name
		if parameter.In == "header" {
			parameterName = strings.ToLower(parameterName)
		}
		key := parameter.In + "\x00" + parameterName
		if seenParameters[key] {
			return fmt.Errorf("parameter %q in %s is duplicated", parameter.Name, parameter.In)
		}
		seenParameters[key] = true
	}
	if operation.RequestBody != nil && len(operation.RequestBody.Content) == 0 {
		return errors.New("request body needs at least one content type")
	}
	for status, response := range operation.Responses {
		for name, header := range response.Headers {
			if header.Schema == nil {
				return fmt.Errorf("response %s header %q needs a schema", status, name)
			}
		}
	}
	return nil
}

func validResponseStatus(status string) bool {
	if status == "default" {
		return true
	}
	if len(status) != 3 || status[0] < '1' || status[0] > '5' {
		return false
	}
	if status[1] == 'X' || status[2] == 'X' {
		return status[1:] == "XX"
	}
	for _, character := range status[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validatePathParameters(path string, parameters []Parameter) error {
	names := make(map[string]bool)
	remaining := path
	for {
		start := strings.IndexByte(remaining, '{')
		if start < 0 {
			break
		}
		if strings.Contains(remaining[:start], "}") {
			return errors.New("malformed path parameter")
		}
		end := strings.IndexByte(remaining[start+1:], '}')
		if end < 0 {
			return errors.New("malformed path parameter")
		}
		name := remaining[start+1 : start+1+end]
		if name == "" || strings.ContainsAny(name, "{/") {
			return errors.New("malformed path parameter")
		}
		if names[name] {
			return fmt.Errorf("duplicate path parameter %q", name)
		}
		names[name] = true
		remaining = remaining[start+end+2:]
	}
	if strings.Contains(remaining, "}") {
		return errors.New("malformed path parameter")
	}

	documented := make(map[string]bool)
	for _, parameter := range parameters {
		if parameter.In != "path" {
			continue
		}
		if !names[parameter.Name] {
			return fmt.Errorf("path parameter %q is not present in the path", parameter.Name)
		}
		if !parameter.Required {
			return fmt.Errorf("path parameter %q must be required", parameter.Name)
		}
		if documented[parameter.Name] {
			return fmt.Errorf("path parameter %q is documented more than once", parameter.Name)
		}
		documented[parameter.Name] = true
	}
	for name := range names {
		if !documented[name] {
			return fmt.Errorf("path parameter %q is not documented", name)
		}
	}
	return nil
}

func cloneOperation(operation Operation) (Operation, error) {
	encoded, err := json.Marshal(operation)
	if err != nil {
		return Operation{}, err
	}
	var result Operation
	if err := decodeJSONNumber(encoded, &result); err != nil {
		return Operation{}, err
	}
	return result, nil
}

func decodeJSONNumber(encoded []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	return decoder.Decode(destination)
}

// MustAdd registers an operation and panics on failure.
func (r *Registry) MustAdd(path, method string, operation Operation) {
	if err := r.Add(path, method, operation); err != nil {
		panic(err)
	}
}

// Remove deletes a documented operation. It is primarily useful for rolling
// back a multi-registry registration when the HTTP router rejects the route.
func (r *Registry) Remove(path, method string) {
	method = strings.ToLower(strings.TrimSpace(method))
	r.mu.Lock()
	defer r.mu.Unlock()
	if operation, exists := r.document.Paths[path][method]; exists && operation.OperationID != "" {
		delete(r.operationIDs, operation.OperationID)
	}
	delete(r.document.Paths[path], method)
	if len(r.document.Paths[path]) == 0 {
		delete(r.document.Paths, path)
	}
}

// AddSecurityScheme registers or replaces a validated security scheme.
func (r *Registry) AddSecurityScheme(name string, scheme SecurityScheme) error {
	if err := validateSecurityScheme(name, scheme); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidOperation, err)
	}
	r.mu.Lock()
	r.document.Components.SecuritySchemes[name] = scheme
	r.mu.Unlock()
	return nil
}

func validateSecurityScheme(name string, scheme SecurityScheme) error {
	if !validComponentName(name) {
		return fmt.Errorf("invalid security scheme name %q", name)
	}
	switch scheme.Type {
	case "apiKey":
		if scheme.Name == "" {
			return errors.New("apiKey security scheme needs a name")
		}
		switch scheme.In {
		case "header", "query", "cookie":
		default:
			return fmt.Errorf("apiKey security scheme has invalid location %q", scheme.In)
		}
		if scheme.Scheme != "" || scheme.BearerFormat != "" {
			return errors.New("apiKey security scheme contains HTTP-only fields")
		}
	case "http":
		if strings.TrimSpace(scheme.Scheme) == "" {
			return errors.New("http security scheme needs a scheme")
		}
		if scheme.BearerFormat != "" && !strings.EqualFold(scheme.Scheme, "bearer") {
			return errors.New("bearerFormat requires the bearer HTTP scheme")
		}
		if scheme.Name != "" || scheme.In != "" {
			return errors.New("http security scheme contains apiKey-only fields")
		}
	case "mutualTLS":
		if scheme.Name != "" || scheme.In != "" || scheme.Scheme != "" || scheme.BearerFormat != "" {
			return errors.New("mutualTLS security scheme contains fields for another scheme type")
		}
	default:
		return fmt.Errorf("unsupported security scheme type %q", scheme.Type)
	}
	return nil
}

func validComponentName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func (r *Registry) validateSecurityRequirementsLocked(requirements []SecurityRequirement) error {
	for _, requirement := range requirements {
		for name, scopes := range requirement {
			scheme, exists := r.document.Components.SecuritySchemes[name]
			if !exists {
				return fmt.Errorf("security requirement references unknown scheme %q", name)
			}
			if len(scopes) > 0 && scheme.Type != "oauth2" && scheme.Type != "openIdConnect" {
				return fmt.Errorf("security scheme %q does not accept scopes", name)
			}
		}
	}
	return nil
}

// SetServers replaces the document's server list.
func (r *Registry) SetServers(servers ...Server) {
	r.mu.Lock()
	r.document.Servers = append([]Server(nil), servers...)
	r.mu.Unlock()
}

// SetTags replaces the document's tag list.
func (r *Registry) SetTags(tags ...Tag) {
	r.mu.Lock()
	r.document.Tags = append([]Tag(nil), tags...)
	r.mu.Unlock()
}

// SchemaForType generates a schema for t and adds reusable named structs to
// document components.
func (r *Registry) SchemaForType(t reflect.Type) *Schema {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.schemaForTypeLocked(t, make(map[reflect.Type]bool))
}

// SchemaForTypeWithValidation generates a schema and applies the typed
// validation tag to it. It is used for non-body parameters whose constraints
// live on the transport wrapper field rather than on a JSON object property.
func (r *Registry) SchemaForTypeWithValidation(t reflect.Type, validation string) *Schema {
	r.mu.Lock()
	defer r.mu.Unlock()
	schema := r.schemaForTypeLocked(t, make(map[reflect.Type]bool))
	applyValidationSchema(schema, validation, t)
	return schema
}

// SchemaFor generates a schema for T and adds named structs to the registry.
func SchemaFor[T any](registry *Registry) *Schema {
	if registry == nil {
		registry = New(Info{})
	}
	return registry.SchemaForType(reflect.TypeFor[T]())
}

// Document returns a deep snapshot of the current document.
func (r *Registry) Document() Document {
	r.mu.RLock()
	defer r.mu.RUnlock()

	encoded, err := json.Marshal(r.document)
	if err != nil {
		return Document{}
	}
	var snapshot Document
	if err := decodeJSONNumber(encoded, &snapshot); err != nil {
		return Document{}
	}
	return snapshot
}

// JSON returns a deterministic, indented OpenAPI document.
func (r *Registry) JSON() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return json.MarshalIndent(r.document, "", "  ")
}

// YAML returns a deterministic YAML 1.2 representation.
func (r *Registry) YAML() ([]byte, error) {
	document, err := r.JSON()
	if err != nil {
		return nil, err
	}
	return marshalYAML(document)
}

// JSONHandler serves the current JSON document.
func (r *Registry) JSONHandler() http.Handler {
	return documentHandler("application/json; charset=utf-8", r.JSON)
}

// YAMLHandler serves the current YAML 1.2 document.
func (r *Registry) YAMLHandler() http.Handler {
	return documentHandler("application/yaml; charset=utf-8", r.YAML)
}

func documentHandler(contentType string, marshal func() ([]byte, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		payload, err := marshal()
		if err != nil {
			http.Error(w, "failed to generate API document", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(payload)
	})
}

var swaggerPage = template.Must(template.New("swagger").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>window.ui = SwaggerUIBundle({url: {{.SpecURL}}, dom_id: '#swagger-ui', deepLinking: true});</script>
</body>
</html>`))

// SwaggerUI returns a handler for Swagger UI. Assets are loaded from the
// major-version-constrained swagger-ui-dist CDN; the API document remains application
// owned and is fetched from specURL.
func SwaggerUI(specURL, title string) http.Handler {
	if specURL == "" {
		specURL = "/openapi.json"
	}
	if title == "" {
		title = "API documentation"
	}
	data := struct {
		Title   string
		SpecURL template.JS
	}{
		Title:   title,
		SpecURL: template.JS(strconvQuote(specURL)), // #nosec G203 -- JSON quoted before trusted template insertion.
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := swaggerPage.Execute(w, data); err != nil {
			http.Error(w, "failed to render Swagger UI", http.StatusInternalServerError)
		}
	})
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func supportedMethod(method string) bool {
	switch method {
	case "get", "put", "post", "delete", "options", "head", "patch", "trace":
		return true
	default:
		return false
	}
}
