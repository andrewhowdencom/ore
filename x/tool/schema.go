package tool

import (
	"encoding/json"
	"fmt"
)

// ValidateSchema checks that a JSON Schema intended for OpenAI-compatible
// function calling is structurally sound. It returns a descriptive error
// if the schema violates common requirements.
//
// Rules:
//   - nil or empty schema is valid (no parameters).
//   - The schema must be JSON-serializable.
//   - If the schema has any keys, the root type must be "object".
//   - All top-level keys must be known JSON Schema keywords; any other key
//     is treated as a misplaced parameter definition and results in an error.
func ValidateSchema(schema map[string]any) error {
	if len(schema) == 0 {
		return nil
	}

	if _, err := json.Marshal(schema); err != nil {
		return fmt.Errorf("schema is not JSON-serializable: %w", err)
	}

	t, ok := schema["type"].(string)
	if !ok || t != "object" {
		return fmt.Errorf("schema root type must be \"object\" for function parameters, got %v", schema["type"])
	}

	for key := range schema {
		if !knownSchemaKeywords[key] {
			return fmt.Errorf("schema contains unknown top-level key %q; parameter definitions must be nested under \"properties\"", key)
		}
	}

	return nil
}

// knownSchemaKeywords is the set of recognized JSON Schema draft-07 and
// draft-2020-12 keywords that may appear at the root of a tool parameter
// schema. Any other top-level key is treated as a misplaced parameter
// definition.
var knownSchemaKeywords = map[string]bool{
	// Type and object structure.
	"type":                     true,
	"properties":               true,
	"required":                 true,
	"additionalProperties":     true,
	"patternProperties":        true,
	"propertyNames":            true,
	"minProperties":            true,
	"maxProperties":            true,
	"unevaluatedProperties":    true,

	// Array.
	"items":                    true,
	"prefixItems":              true,
	"contains":                 true,
	"minContains":              true,
	"maxContains":              true,
	"minItems":                 true,
	"maxItems":                 true,
	"uniqueItems":              true,

	// Validation.
	"enum":                     true,
	"const":                    true,
	"format":                   true,
	"multipleOf":               true,
	"maximum":                  true,
	"exclusiveMaximum":         true,
	"minimum":                  true,
	"exclusiveMinimum":         true,
	"maxLength":                true,
	"minLength":                true,
	"pattern":                  true,

	// Composition.
	"allOf":                    true,
	"anyOf":                    true,
	"oneOf":                    true,
	"not":                      true,
	"if":                       true,
	"then":                     true,
	"else":                     true,
	"dependentSchemas":         true,
	"dependentRequired":        true,

	// Meta.
	"title":                    true,
	"description":              true,
	"default":                  true,
	"examples":                 true,
	"deprecated":               true,
	"readOnly":                 true,
	"writeOnly":                true,
	"$schema":                  true,
	"$id":                      true,
	"$ref":                     true,
	"$defs":                    true,
	"definitions":              true,
	"$anchor":                  true,
	"$dynamicRef":              true,
	"$dynamicAnchor":           true,
	"$vocabulary":              true,
	"$comment":                 true,

	// Content.
	"contentMediaType":         true,
	"contentEncoding":          true,
}
