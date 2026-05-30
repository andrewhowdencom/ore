// Package provider defines the Provider interface, the contract between the core
// loop and concrete LLM provider adapters.
package provider

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
)

// InvokeOption is a marker interface for per-invocation configuration options.
// Concrete provider sub-packages define their own option types and exported
// constructors (e.g. openai.WithTools). Providers silently ignore options they
// do not recognize.
type InvokeOption interface {
	IsInvokeOption()
}

// ToolsOption is a per-invocation option that configures available tools.
type ToolsOption struct {
	Tools func(ctx context.Context, st state.State) []Tool
}

// IsInvokeOption marks ToolsOption as a provider.InvokeOption.
func (ToolsOption) IsInvokeOption() {}

// WithTools returns an InvokeOption that configures the set of available tools
// for a single provider invocation.
func WithTools(tools []Tool) InvokeOption {
	return ToolsOption{Tools: func(context.Context, state.State) []Tool { return tools }}
}

// Provider is the interface implemented by LLM provider adapters.
type Provider interface {
	// Invoke serializes the given state, calls the LLM API, and emits
	// deserialized response artifacts to the provided channel.
	//
	// The adapter must emit each artifact as soon as the native API delivers a
	// chunk, preserving that arrival order.
	//
	// Adapters must emit canonical ore artifact types as soon as the native
	// API delivers a chunk, preserving that arrival order. Fragmented data
	// (e.g. streaming text or tool-call chunks) is emitted as delta types
	// (TextDelta, ToolCallDelta, etc.) and accumulated by the core loop.
	// Adapters should not perform their own accumulation except when the
	// native format genuinely cannot be expressed as an ore artifact type.
	//
	// The channel must not be closed by the adapter.
	Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...InvokeOption) error
}

// Tool describes a callable tool exposed to an LLM provider.
type Tool struct {
	Name        string
	Description string
	// Schema defines the JSON Schema for the tool's parameters.
	Schema map[string]any
}


