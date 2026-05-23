package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSchema(t *testing.T) {
	tests := []struct {
		name    string
		schema  map[string]any
		wantErr string
	}{
		{
			name:   "nil schema",
			schema: nil,
		},
		{
			name:   "empty map",
			schema: map[string]any{},
		},
		{
			name:   "valid object type",
			schema: map[string]any{"type": "object"},
		},
		{
			name: "valid with properties and required",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			name:    "non-serializable channel value",
			schema:  map[string]any{"type": make(chan int)},
			wantErr: "schema is not JSON-serializable",
		},
		{
			name:    "missing root type",
			schema:  map[string]any{"properties": map[string]any{}},
			wantErr: `schema root type must be "object"`,
		},
		{
			name:    "root type is string",
			schema:  map[string]any{"type": "string"},
			wantErr: `schema root type must be "object"`,
		},
		{
			name:    "root type is number",
			schema:  map[string]any{"type": "number"},
			wantErr: `schema root type must be "object"`,
		},
		{
			name: "misplaced property definition missing root type",
			schema: map[string]any{
				"name": map[string]any{"type": "string"},
			},
			wantErr: `schema root type must be "object"`,
		},
		{
			name: "misplaced property with valid object type",
			schema: map[string]any{
				"type": "object",
				"name": map[string]any{"type": "string"},
			},
			wantErr: `schema contains unknown top-level key "name"`,
		},
		{
			name: "circular reference",
			schema: func() map[string]any {
				m := map[string]any{"type": "object"}
				m["self"] = m
				return m
			}(),
			wantErr: "schema is not JSON-serializable",
		},
		{
			name: "valid with all known keywords",
			schema: map[string]any{
				"type":                     "object",
				"properties":               map[string]any{},
				"required":                 []string{},
				"additionalProperties":     true,
				"patternProperties":        map[string]any{},
				"propertyNames":            map[string]any{},
				"minProperties":            0.0,
				"maxProperties":            0.0,
				"unevaluatedProperties":    true,
				"items":                    map[string]any{},
				"prefixItems":              []any{},
				"contains":                 map[string]any{},
				"minContains":              0.0,
				"maxContains":              0.0,
				"minItems":                 0.0,
				"maxItems":                 0.0,
				"uniqueItems":              true,
				"enum":                     []any{},
				"const":                    "x",
				"format":                   "email",
				"multipleOf":               1.0,
				"maximum":                  1.0,
				"exclusiveMaximum":         1.0,
				"minimum":                  1.0,
				"exclusiveMinimum":         1.0,
				"maxLength":                1.0,
				"minLength":                1.0,
				"pattern":                  ".*",
				"allOf":                    []any{},
				"anyOf":                    []any{},
				"oneOf":                    []any{},
				"not":                      map[string]any{},
				"if":                       map[string]any{},
				"then":                     map[string]any{},
				"else":                     map[string]any{},
				"dependentSchemas":         map[string]any{},
				"dependentRequired":        map[string]any{},
				"title":                    "title",
				"description":              "desc",
				"default":                  "def",
				"examples":                 []any{},
				"deprecated":               true,
				"readOnly":                 true,
				"writeOnly":                true,
				"$schema":                  "http://example.com",
				"$id":                      "id",
				"$ref":                     "#/ref",
				"$defs":                    map[string]any{},
				"definitions":              map[string]any{},
				"$anchor":                  "anchor",
				"$dynamicRef":              "#/dynamic",
				"$dynamicAnchor":           "dyn",
				"$vocabulary":              map[string]any{},
				"$comment":                 "comment",
				"contentMediaType":         "text/plain",
				"contentEncoding":          "base64",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSchema(tt.schema)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
