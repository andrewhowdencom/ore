// Package provider defines the Provider interface, the contract between the
// core loop and concrete LLM provider adapters.
//
// The provider contract is intentionally minimal: a single Invoke() method
// that streams artifacts on the supplied channel in the exact order they are
// received from the underlying LLM API. Adapters must emit canonical ore
// artifact types (including delta types such as TextDelta, ToolCallDelta, and
// ReasoningDelta) immediately; the core loop accumulates them into complete
// artifacts. Adapters should not perform their own accumulation except when
// the native format genuinely cannot be expressed as an ore artifact type.
//
// See the Provider interface in provider.go for the full contract and the
// artifact package for the list of canonical types.
package provider
