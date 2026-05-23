// Package calculator provides reusable calculator tool implementations for the
// ore tool extension.
//
// It exports pre-built Add and Multiply tool functions together with their
// provider.Tool JSON-schema descriptors, so applications can register them in a
// tool.Registry without defining the logic inline.
//
// Usage:
//
//	registry := tool.NewRegistry()
//	if err := registry.Register(AddTool.Name, AddTool.Description, AddTool.Schema, Add); err != nil {
//	    ...
//	}
//	if err := registry.Register(MultiplyTool.Name, MultiplyTool.Description, MultiplyTool.Schema, Multiply); err != nil {
//	    ...
//	}
//
//	// Registry.Tools() is the single source of truth for the provider.
//	tools := registry.Tools()
package calculator
