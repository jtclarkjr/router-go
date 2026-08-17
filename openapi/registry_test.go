package openapi_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/jtclarkjr/router-go/openapi"
)

type node struct {
	ID       string  `json:"id" validate:"required" description:"Node identifier" example:"node-1"`
	Weight   float64 `json:"weight,omitempty" validate:"min=0,max=10"`
	Category string  `json:"category" validate:"oneof=primary secondary"`
	Next     *node   `json:"next,omitempty"`
}

type collectionConstraints struct {
	Tags     []string          `json:"tags" validate:"min=1,max=3"`
	Metadata map[string]string `json:"metadata" validate:"len=2"`
	Level    int               `json:"level" validate:"oneof=1 2 3"`
}

type requiredPointers struct {
	Enabled *bool   `json:"enabled" validate:"required"`
	Count   *int    `json:"count" validate:"required"`
	Name    *string `json:"name" validate:"required"`
}

type envelope[T any] struct {
	Data T `json:"data" validate:"required"`
}

type Cookie struct {
	Value string `json:"value"`
}

type embeddedFields struct {
	TraceID string `json:"traceId" validate:"required"`
}

type embeddedPayload struct {
	*embeddedFields
}

func firstLocalCollisionType() reflect.Type {
	type LocalCollision struct {
		First string `json:"first"`
	}
	return reflect.TypeFor[LocalCollision]()
}

func secondLocalCollisionType() reflect.Type {
	type LocalCollision struct {
		Second int `json:"second"`
	}
	return reflect.TypeFor[LocalCollision]()
}

func TestRegistryGoldenDocument(t *testing.T) {
	registry := openapi.New(openapi.Info{Title: "Graph API", Version: "0.7.0", Description: "Typed graph operations"})
	registry.SetServers(openapi.Server{URL: "https://api.example.test"})
	registry.SetTags(openapi.Tag{Name: "nodes", Description: "Graph nodes"})
	if err := registry.AddSecurityScheme("bearerAuth", openapi.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"}); err != nil {
		t.Fatalf("AddSecurityScheme: %v", err)
	}

	nodeSchema := registry.SchemaForType(reflect.TypeFor[node]())
	err := registry.Add("/nodes/{id}", http.MethodGet, openapi.Operation{
		OperationID: "getNode",
		Summary:     "Get a node",
		Tags:        []string{"nodes"},
		Parameters: []openapi.Parameter{{
			Name:     "id",
			In:       "path",
			Required: true,
			Schema:   &openapi.Schema{Type: "string"},
		}},
		Responses: map[string]openapi.Response{
			"200": {
				Description: "Node found",
				Content: map[string]openapi.MediaType{
					"application/json": {Schema: nodeSchema},
				},
			},
		},
		Security:   []openapi.SecurityRequirement{{"bearerAuth": {}}},
		Extensions: map[string]any{"x-audit": true},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := registry.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile("testdata/graph_api.golden.json", append(got, '\n'), 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
	}
	want, err := os.ReadFile("testdata/graph_api.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("OpenAPI document mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	yaml, err := registry.YAML()
	if err != nil {
		t.Fatalf("YAML: %v", err)
	}
	secondYAML, err := registry.YAML()
	if err != nil {
		t.Fatalf("second YAML: %v", err)
	}
	if bytes.Equal(yaml, got) || !bytes.Equal(yaml, secondYAML) {
		t.Fatal("YAML must be distinct from JSON and byte-for-byte deterministic")
	}
	for _, fragment := range []string{`"openapi": "3.1.0"`, `"/nodes/{id}":`, `"bearerAuth": []`} {
		if !bytes.Contains(yaml, []byte(fragment)) {
			t.Fatalf("YAML is missing %q:\n%s", fragment, yaml)
		}
	}
	if audit, ok := registry.Document().Paths["/nodes/{id}"]["get"].Extensions["x-audit"].(bool); !ok || !audit {
		t.Fatal("document snapshot did not preserve operation extensions")
	}
}

func TestRegistryErrorsSnapshotsAndRemoval(t *testing.T) {
	registry := openapi.New(openapi.Info{})
	operation := openapi.Operation{Responses: map[string]openapi.Response{"204": {Description: "Done"}}}
	if err := registry.Add("invalid", http.MethodGet, operation); !errors.Is(err, openapi.ErrInvalidOperation) {
		t.Fatalf("invalid path error = %v", err)
	}
	if err := registry.Add("/invalid/*", http.MethodGet, operation); !errors.Is(err, openapi.ErrInvalidOperation) {
		t.Fatalf("wildcard path error = %v", err)
	}
	if err := registry.Add("/invalid/{id}", http.MethodGet, operation); !errors.Is(err, openapi.ErrInvalidOperation) {
		t.Fatalf("undocumented path parameter error = %v", err)
	}
	if err := registry.Add("/connect", http.MethodConnect, operation); !errors.Is(err, openapi.ErrInvalidOperation) {
		t.Fatalf("invalid method error = %v", err)
	}
	if err := registry.Add("/healthz", http.MethodGet, operation); err != nil {
		t.Fatalf("Add: %v", err)
	}
	operation.Responses["500"] = openapi.Response{Description: "mutated by caller"}
	if _, exists := registry.Document().Paths["/healthz"]["get"].Responses["500"]; exists {
		t.Fatal("registry retained caller-owned response maps")
	}
	if err := registry.Add("/healthz", http.MethodGet, operation); !errors.Is(err, openapi.ErrDuplicateOperation) {
		t.Fatalf("duplicate error = %v", err)
	}
	identified := openapi.Operation{OperationID: "unique", Responses: map[string]openapi.Response{"200": {Description: "OK"}}}
	if err := registry.Add("/one", http.MethodGet, identified); err != nil {
		t.Fatalf("identified Add: %v", err)
	}
	if err := registry.Add("/two", http.MethodGet, identified); !errors.Is(err, openapi.ErrDuplicateOperation) {
		t.Fatalf("duplicate operationId error = %v", err)
	}
	if _, exists := registry.Document().Paths["/two"]; exists {
		t.Fatal("failed operationId registration left an empty path")
	}
	registry.Remove("/one", http.MethodGet)
	if err := registry.Add("/two", http.MethodGet, identified); err != nil {
		t.Fatalf("operationId was not released by Remove: %v", err)
	}

	snapshot := registry.Document()
	delete(snapshot.Paths, "/healthz")
	if _, exists := registry.Document().Paths["/healthz"]; !exists {
		t.Fatal("mutating a snapshot changed the registry")
	}
	registry.Remove("/healthz", http.MethodGet)
	if _, exists := registry.Document().Paths["/healthz"]; exists {
		t.Fatal("Remove left an empty path")
	}
}

func TestDocumentAndSwaggerHandlers(t *testing.T) {
	registry := openapi.New(openapi.Info{Title: "Test", Version: "1"})
	jsonResponse := httptest.NewRecorder()
	registry.JSONHandler().ServeHTTP(jsonResponse, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if jsonResponse.Code != http.StatusOK || jsonResponse.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("JSON handler = %d %q", jsonResponse.Code, jsonResponse.Header().Get("Content-Type"))
	}

	swagger := httptest.NewRecorder()
	openapi.SwaggerUI(`</script><script>alert(1)</script>`, "Docs").ServeHTTP(swagger, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if swagger.Code != http.StatusOK || !strings.Contains(swagger.Body.String(), "SwaggerUIBundle") {
		t.Fatalf("Swagger response = %d %q", swagger.Code, swagger.Body.String())
	}
	if strings.Contains(swagger.Body.String(), "</script><script>alert") {
		t.Fatal("Swagger spec URL was not safely JSON quoted")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	registry := openapi.New(openapi.Info{Title: "Concurrent", Version: "1"})
	operation := openapi.Operation{Responses: map[string]openapi.Response{"200": {Description: "OK"}}}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		i := i
		wg.Add(3)
		go func() {
			defer wg.Done()
			if err := registry.Add(fmt.Sprintf("/items/%d", i), http.MethodGet, operation); err != nil {
				t.Errorf("Add %d: %v", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			_ = registry.SchemaForType(reflect.TypeFor[node]())
		}()
		go func() {
			defer wg.Done()
			if _, err := registry.JSON(); err != nil {
				t.Errorf("JSON: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := len(registry.Document().Paths); got != 100 {
		t.Fatalf("paths = %d, want 100", got)
	}
}

func TestSchemaUsesCollectionAndTypedEnumConstraints(t *testing.T) {
	registry := openapi.New(openapi.Info{})
	ref := registry.SchemaForType(reflect.TypeFor[collectionConstraints]())
	name := strings.TrimPrefix(ref.Ref, "#/components/schemas/")
	schema := registry.Document().Components.Schemas[name]
	if schema.Properties["tags"].MinItems == nil || *schema.Properties["tags"].MinItems != 1 || schema.Properties["tags"].MaxItems == nil || *schema.Properties["tags"].MaxItems != 3 {
		t.Fatalf("array constraints = %#v", schema.Properties["tags"])
	}
	if schema.Properties["metadata"].MinProperties == nil || *schema.Properties["metadata"].MinProperties != 2 || schema.Properties["metadata"].MaxProperties == nil || *schema.Properties["metadata"].MaxProperties != 2 {
		t.Fatalf("map constraints = %#v", schema.Properties["metadata"])
	}
	if got := schema.Properties["level"].Enum; len(got) != 3 || fmt.Sprint(got[0]) != "1" {
		t.Fatalf("integer enum = %#v", got)
	}
}

func TestRequiredPointersOnlyRequirePresence(t *testing.T) {
	registry := openapi.New(openapi.Info{})
	ref := registry.SchemaForType(reflect.TypeFor[requiredPointers]())
	schema := registry.Document().Components.Schemas[strings.TrimPrefix(ref.Ref, "#/components/schemas/")]
	if got := strings.Join(schema.Required, ","); got != "enabled,count,name" {
		t.Fatalf("required fields = %q", got)
	}

	for name, wantType := range map[string]string{"enabled": "boolean", "count": "integer", "name": "string"} {
		property := schema.Properties[name]
		if len(property.OneOf) != 1 || property.OneOf[0].Type != wantType {
			t.Fatalf("%s schema = %#v", name, property)
		}
		target := property.OneOf[0]
		if target.Const != nil || target.Not != nil || target.MinLength != nil {
			t.Fatalf("%s gained a nonzero constraint: %#v", name, target)
		}
	}
}

func TestUnsignedIntegerEnumKeepsExactJSONValue(t *testing.T) {
	type unsignedEnum struct {
		Value uint64 `json:"value" validate:"oneof=18446744073709551615"`
	}
	registry := openapi.New(openapi.Info{})
	ref := registry.SchemaForType(reflect.TypeFor[unsignedEnum]())
	schema := registry.Document().Components.Schemas[strings.TrimPrefix(ref.Ref, "#/components/schemas/")]
	if got := fmt.Sprint(schema.Properties["value"].Enum[0]); got != "18446744073709551615" {
		t.Fatalf("uint64 enum = %q, want exact maximum", got)
	}
	payload, err := registry.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !bytes.Contains(payload, []byte(`18446744073709551615`)) {
		t.Fatalf("exact uint64 enum missing from JSON: %s", payload)
	}
}

func TestSchemaNamesAreValidAndIndependentOfRegistrationOrder(t *testing.T) {
	first := openapi.New(openapi.Info{})
	firstInt := first.SchemaForType(reflect.TypeFor[envelope[int]]()).Ref
	firstString := first.SchemaForType(reflect.TypeFor[envelope[string]]()).Ref
	firstLocalCookie := first.SchemaForType(reflect.TypeFor[Cookie]()).Ref
	firstHTTPCookie := first.SchemaForType(reflect.TypeFor[http.Cookie]()).Ref

	second := openapi.New(openapi.Info{})
	secondHTTPCookie := second.SchemaForType(reflect.TypeFor[http.Cookie]()).Ref
	secondLocalCookie := second.SchemaForType(reflect.TypeFor[Cookie]()).Ref
	secondString := second.SchemaForType(reflect.TypeFor[envelope[string]]()).Ref
	secondInt := second.SchemaForType(reflect.TypeFor[envelope[int]]()).Ref

	if firstInt != secondInt || firstString != secondString || firstInt == firstString ||
		firstLocalCookie != secondLocalCookie || firstHTTPCookie != secondHTTPCookie || firstLocalCookie == firstHTTPCookie {
		t.Fatalf("unstable schema refs: first=(%q,%q) second=(%q,%q)", firstInt, firstString, secondInt, secondString)
	}
	componentPattern := regexp.MustCompile(`^#/components/schemas/[A-Za-z0-9._-]+$`)
	for _, ref := range []string{firstInt, firstString, firstLocalCookie, firstHTTPCookie} {
		if !componentPattern.MatchString(ref) {
			t.Fatalf("invalid component ref: %q", ref)
		}
	}
}

func TestFunctionLocalSchemaNamesCannotCollide(t *testing.T) {
	firstType := firstLocalCollisionType()
	secondType := secondLocalCollisionType()
	if firstType.PkgPath() != secondType.PkgPath() || firstType.String() != secondType.String() {
		t.Fatalf("test types do not exercise the reflect identity collision: %s/%s and %s/%s", firstType.PkgPath(), firstType, secondType.PkgPath(), secondType)
	}

	firstRegistry := openapi.New(openapi.Info{})
	firstRef := firstRegistry.SchemaForType(firstType).Ref
	secondRef := firstRegistry.SchemaForType(secondType).Ref
	secondRegistry := openapi.New(openapi.Info{})
	secondRefReversed := secondRegistry.SchemaForType(secondType).Ref
	firstRefReversed := secondRegistry.SchemaForType(firstType).Ref
	if firstRef == secondRef || firstRef != firstRefReversed || secondRef != secondRefReversed {
		t.Fatalf("local schema refs collide or depend on order: first=(%q,%q) reversed=(%q,%q)", firstRef, secondRef, firstRefReversed, secondRefReversed)
	}

	document := firstRegistry.Document()
	firstSchema := document.Components.Schemas[strings.TrimPrefix(firstRef, "#/components/schemas/")]
	secondSchema := document.Components.Schemas[strings.TrimPrefix(secondRef, "#/components/schemas/")]
	if firstSchema.Properties["first"] == nil || secondSchema.Properties["second"] == nil {
		t.Fatalf("local schema components were overwritten: first=%#v second=%#v", firstSchema, secondSchema)
	}
}

func TestByteArrayUsesJSONArraySchema(t *testing.T) {
	registry := openapi.New(openapi.Info{})
	schema := registry.SchemaForType(reflect.TypeFor[[4]byte]())
	if schema.Type != "array" || schema.Items == nil || schema.Items.Type != "integer" {
		t.Fatalf("byte array schema = %#v, want array of integers", schema)
	}
	byteSlice := registry.SchemaForType(reflect.TypeFor[[]byte]())
	if byteSlice.Type != "string" || byteSlice.Format != "byte" {
		t.Fatalf("byte slice schema = %#v, want base64 string", byteSlice)
	}
}

func TestStructSchemasAreClosedAndFlattenEmbeddedPointers(t *testing.T) {
	registry := openapi.New(openapi.Info{})
	ref := registry.SchemaForType(reflect.TypeFor[embeddedPayload]())
	name := strings.TrimPrefix(ref.Ref, "#/components/schemas/")
	schema := registry.Document().Components.Schemas[name]
	if schema.AdditionalPropertiesAllowed == nil || *schema.AdditionalPropertiesAllowed {
		t.Fatalf("additionalProperties policy = %#v, want false", schema.AdditionalPropertiesAllowed)
	}
	if schema.Properties["traceId"] == nil {
		t.Fatalf("embedded pointer fields missing: %#v", schema.Properties)
	}

	registry.SetStructAdditionalPropertiesAllowed(true)
	schema = registry.Document().Components.Schemas[name]
	if schema.AdditionalPropertiesAllowed == nil || !*schema.AdditionalPropertiesAllowed {
		t.Fatalf("updated additionalProperties policy = %#v, want true", schema.AdditionalPropertiesAllowed)
	}
}

func TestRegistryRejectsIncompleteParametersAndInvalidSecurity(t *testing.T) {
	registry := openapi.New(openapi.Info{})
	operation := openapi.Operation{
		Parameters: []openapi.Parameter{{Name: "filter", In: "query"}},
		Responses:  map[string]openapi.Response{"200": {Description: "OK"}},
	}
	if err := registry.Add("/items", http.MethodGet, operation); !errors.Is(err, openapi.ErrInvalidOperation) {
		t.Fatalf("parameter without schema error = %v", err)
	}
	if err := registry.AddSecurityScheme("bad name", openapi.SecurityScheme{Type: "http", Scheme: "bearer"}); !errors.Is(err, openapi.ErrInvalidOperation) {
		t.Fatalf("invalid security scheme error = %v", err)
	}
	operation.Parameters = nil
	operation.Security = []openapi.SecurityRequirement{{"missing": {}}}
	if err := registry.Add("/items", http.MethodGet, operation); !errors.Is(err, openapi.ErrInvalidOperation) {
		t.Fatalf("unknown security requirement error = %v", err)
	}

	operation.Security = nil
	operation.Parameters = []openapi.Parameter{
		{Name: "filter", In: "query", Schema: &openapi.Schema{Type: "string"}},
		{Name: "Filter", In: "query", Schema: &openapi.Schema{Type: "string"}},
	}
	if err := registry.Add("/items", http.MethodGet, operation); err != nil {
		t.Fatalf("case-sensitive query parameters: %v", err)
	}
}
