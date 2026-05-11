// Package provider defines the Provider interface, the contract between the
// core loop and concrete LLM provider adapters.
//
// The provider contract is intentionally minimal: a single Invoke() method that
// emits artifacts to a caller-provided channel. Metadata (token usage, finish
// reason) can be attached as custom artifact types or inspected by
// type-asserting the concrete provider adapter in the application layer.
package provider
