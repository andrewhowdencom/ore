// Package provider defines the Provider interface, the contract between the
// core loop and concrete LLM provider adapters.
//
// The provider contract is intentionally minimal: a single Invoke() method
// that streams artifacts on the supplied channel in the exact order they are
// received from the underlying LLM API. Adapters may accumulate only when the
// native response cannot be expressed as a single canonical ore artifact (for
// example, fragmented tool-call payloads). All other artifacts — TextDelta,
// ReasoningDelta, etc. — must be emitted immediately.
//
// See the Provider interface in provider.go for the full contract and the
// artifact package for the list of canonical types.
package provider
