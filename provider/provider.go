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

// Provider is the interface implemented by LLM provider adapters.
type Provider interface {
	// Invoke serializes the given state, calls the LLM API, and emits
	// deserialized response artifacts to the provided channel.
	//
	// The adapter must emit each artifact as soon as the native API delivers a
	// chunk, preserving that arrival order.
	//
	// Accumulation is allowed only when the native format cannot be
	// represented directly as a canonical ore artifact (e.g. fragmented
	// tool-call data). In such cases the adapter should assemble the
	// complete artifact before sending it on the channel.
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


