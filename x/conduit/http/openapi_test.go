package http

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// openAPISpec is a lazily-loaded, cached OpenAPI document for test-time
// validation. Tests that need to validate JSON against the spec use
// validateAgainstSchema, which loads the document once and reuses it.
var openAPISpec *openapi3.T

// loadOpenAPISpec loads and validates the openapi.yaml document. It caches
// the result on first call and returns the cached document on subsequent
// calls.
func loadOpenAPISpec(t *testing.T) *openapi3.T {
	t.Helper()
	if openAPISpec != nil {
		return openAPISpec
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	spec, err := loader.LoadFromFile("openapi.yaml")
	if err != nil {
		t.Fatalf("failed to load openapi.yaml: %v", err)
	}

	if err := spec.Validate(loader.Context); err != nil {
		t.Fatalf("openapi.yaml failed validation: %v", err)
	}

	openAPISpec = spec
	return openAPISpec
}

// validateAgainstSchema validates JSON data against a named schema in the
// OpenAPI spec. The schemaName is the name of a schema under
// components/schemas (e.g., "TextEvent", "ErrorEvent").
func validateAgainstSchema(t *testing.T, schemaName string, data []byte) {
	t.Helper()

	spec := loadOpenAPISpec(t)
	schemaRef, ok := spec.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("schema %q not found in openapi.yaml", schemaName)
	}

	schema := schemaRef.Value
	if schema == nil {
		t.Fatalf("schema %q has nil value", schemaName)
	}

	var doc interface{}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		t.Fatalf("failed to decode JSON data: %v", err)
	}

	if err := schema.VisitJSON(doc); err != nil {
		t.Fatalf("JSON data does not match schema %q: %v", schemaName, err)
	}
}
