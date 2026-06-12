package calculator

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

// AddTool is the tool.Tool descriptor (including JSON schema) for Add.
var AddTool = tool.Tool{
	Name:        "add",
	Description: "Add two numbers together. Returns the numeric sum (no truncation needed; output is a single number).",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "number", "description": "The first number"},
			"b": map[string]any{"type": "number", "description": "The second number"},
		},
		"required": []string{"a", "b"},
	},
	DisplayHint: func(args map[string]any) any {
		a := ToFloat64(args["a"])
		b := ToFloat64(args["b"])
		return fmt.Sprintf("%.0f + %.0f = ?", a, b)
	},
	Format: tool.Format{
		// Calculator outputs are intentionally small (a single
		// float64). The zero-value caps trigger the framework
		// default (50 KB / 2000 lines) at handler-application
		// time, which is large enough that a float64 result
		// never gets truncated. The Format declaration is
		// present for documentation and consistency.
	},
}

// MultiplyTool is the tool.Tool descriptor (including JSON schema) for Multiply.
var MultiplyTool = tool.Tool{
	Name:        "multiply",
	Description: "Multiply two numbers together. Returns the numeric product (no truncation needed; output is a single number).",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "number", "description": "The first number"},
			"b": map[string]any{"type": "number", "description": "The second number"},
		},
		"required": []string{"a", "b"},
	},
	DisplayHint: func(args map[string]any) any {
		a := ToFloat64(args["a"])
		b := ToFloat64(args["b"])
		return fmt.Sprintf("%.0f × %.0f = ?", a, b)
	},
	Format: tool.Format{
		// See AddTool.Format for rationale.
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
