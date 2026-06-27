// Package llmbytes computes the LLM-facing payload byte size for an
// artifact. It is the shared canonical implementation that both
// x/analytics and x/telemetry consume; the previous duplication of
// this logic across two packages caused issue #416 (the pointer-case
// regression) because the two copies had to be patched in lockstep.
//
// The contract is: report the size of the payload that is actually
// sent to / received from the LLM provider, NOT the size of the
// on-disk JSON envelope. For Text, Reasoning, Image, and the LLMString()
// of ToolCall/ToolResult, the answer is just len(some-string). For
// Usage, the answer is always 0 (token counts are a separate metric).
// For unknown / custom artifact types, the fallback marshals the
// artifact to JSON and reports the envelope length — best-effort, but
// never wrong about the *minimum* the LLM sees.
//
// The framework guarantees that artifacts reaching this function are
// value-typed: junk/serialize.go's unmarshalArtifacts dereferences
// the factory pointer before storing into the returned slice, so the
// round-trip path and the in-memory path produce the same concrete
// type at the slice boundary. Pointer-typed artifacts are therefore
// not expected here.
package llmbytes

import (
	"encoding/json"

	"github.com/andrewhowdencom/ore/artifact"
)

// Of returns the LLM-facing payload byte size for the given artifact.
//
// The function never panics. For unknown artifact kinds (custom
// implementations of artifact.Artifact), it falls back to the JSON
// envelope length, which is the worst-case size the LLM ever sees for
// the artifact and never smaller than the actual payload.
func Of(art artifact.Artifact) int64 {
	switch a := art.(type) {
	case artifact.Text:
		return int64(len(a.Content))
	case artifact.Reasoning:
		return int64(len(a.Content))
	case artifact.ToolCall:
		return int64(len(a.LLMString()))
	case artifact.ToolResult:
		return int64(len(a.LLMString()))
	case artifact.Image:
		return int64(len(a.URL))
	case artifact.Usage:
		return 0
	default:
		if b, err := json.Marshal(art); err == nil {
			return int64(len(b))
		}
		return 0
	}
}
