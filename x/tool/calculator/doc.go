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
//	registry.Register(calculator.AddTool.Name, calculator.Add)
//	registry.Register(calculator.MultiplyTool.Name, calculator.Multiply)
//
//	// Pass the schemas to the provider.
//	tools := []provider.Tool{calculator.AddTool, calculator.MultiplyTool}
package calculator
