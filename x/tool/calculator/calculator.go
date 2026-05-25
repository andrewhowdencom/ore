package calculator

import (
	"context"
	"strconv"
	"strings"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/tool"
)

// Compile-time type checks.
var _ tool.ToolFunc = Add
var _ tool.ToolFunc = Multiply

// Add adds two numbers parsed from JSON arguments.
func Add(ctx context.Context, _ tool.Sandbox, args map[string]any) (any, error) {
	a := ToFloat64(args["a"])
	b := ToFloat64(args["b"])
	return a + b, nil
}

// Multiply multiplies two numbers parsed from JSON arguments.
func Multiply(ctx context.Context, _ tool.Sandbox, args map[string]any) (any, error) {
	a := ToFloat64(args["a"])
	b := ToFloat64(args["b"])
	return a * b, nil
}

// AddTool is the provider.Tool descriptor (including JSON schema) for Add.
var AddTool = provider.Tool{
	Name:        "add",
	Description: "Add two numbers together",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "number", "description": "The first number"},
			"b": map[string]any{"type": "number", "description": "The second number"},
		},
		"required": []string{"a", "b"},
	},
}

// MultiplyTool is the provider.Tool descriptor (including JSON schema) for Multiply.
var MultiplyTool = provider.Tool{
	Name:        "multiply",
	Description: "Multiply two numbers together",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "number", "description": "The first number"},
			"b": map[string]any{"type": "number", "description": "The second number"},
		},
		"required": []string{"a", "b"},
	},
}

// ToFloat64 converts a JSON-decoded number (or string) to float64.
// Unrecognized types and unparseable strings silently return 0.
func ToFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	}
	return 0
}
