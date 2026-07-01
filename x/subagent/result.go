package subagent

import (
	"encoding/json"
	"fmt"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/x/systemprompt"
)

// Status is the outcome reported by a sub-agent's structured Result.
// The three values partition the response space: "success" indicates
// the sub-agent fully completed the requested task; "partial"
// indicates the sub-agent made progress but did not finish; "failed"
// indicates the sub-agent could not accomplish the task.
//
// AsTool's closure enforces Status at the schema boundary: any
// other value (including the empty string) is rejected as invalid
// and surfaces as a Result with Status set to StatusFailed.
type Status string

const (
	// StatusSuccess is the value of Result.Status when the sub-agent
	// fully completed the requested task.
	StatusSuccess Status = "success"
	// StatusPartial is the value of Result.Status when the sub-agent
	// made progress but did not finish.
	StatusPartial Status = "partial"
	// StatusFailed is the value of Result.Status when the sub-agent
	// could not accomplish the task, or when the sub-agent's output
	// failed schema validation.
	StatusFailed Status = "failed"
)

// Result is the structured outcome of a sub-agent invocation. It is
// returned by the tool-callable ToolFunc produced by AsTool when the
// child's output passes JSON schema validation; malformed output
// (a non-JSON textual response) surfaces as a tool error instead.
// A parseable-but-schema-invalid payload (e.g., a bad enum value or
// a missing required field) surfaces as Result{Status: StatusFailed,
// Summary: <raw payload>} so the parent can branch on outcome
// without losing the diagnostic text.
//
// Findings is an opaque, optional bag for structured data the
// sub-agent wants to convey beyond the prose summary. The protocol
// does not constrain the shape of Findings; downstream consumers
// are expected to type-assert or re-validate as appropriate. The
// JSON schema permits Findings to be absent, present-and-null, or
// present-and-an-object; anything else is invalid.
type Result struct {
	Status   Status
	Summary  string
	Findings any
}

// ResultSchema is the JSON Schema that a sub-agent's textual output
// MUST conform to. AsTool validates the produced turn against this
// schema before constructing a Result. JSON parse failures are
// surfaced as tool errors (the closure returns a non-nil error); a
// parseable payload that fails schema validation is returned as a
// Result with Status set to StatusFailed.
//
// The schema is exported so callers can reuse it for client-side
// validation, documentation, or constrained-decoding
// configurations on providers that support grammar-constrained
// generation. Constrained decoding is not currently wired by AsTool
// — it is a future per-provider extension orthogonal to this
// protocol.
var ResultSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"status": map[string]any{
			"enum": []string{string(StatusSuccess), string(StatusPartial), string(StatusFailed)},
			"type": "string",
		},
		"summary": map[string]any{
			"type": "string",
		},
		"findings": map[string]any{
			"type":     "object",
			"nullable": true,
		},
	},
	"required":             []string{"status", "summary"},
	"additionalProperties": false,
}

// ResultSystemPrompt returns a loop.Transform that injects a
// RoleSystem turn instructing the sub-agent to emit a JSON object
// matching ResultSchema. The factory caller of AsTool should
// include this transform in the child agent's options via
// agent.WithTransforms, e.g.:
//
//	sp, _ := subagent.ResultSystemPrompt()
//	return agent.New("researcher", agent.WithTransforms(sp), ...), nil
//
// The transform's content is rendered once at construction time
// using the current value of ResultSchema. If callers mutate
// ResultSchema at runtime (rare; it is a package-level constant),
// they must call ResultSystemPrompt again to refresh the prompt.
//
// Why a helper: AsTool cannot inject the system-prompt transform
// post-construction because agent.Agent does not expose transform
// mutation. Having the factory include the transform at build
// time keeps the protocol's contract visible at the call site
// without adding new surface on agent.Agent.
func ResultSystemPrompt() (loop.Transform, error) {
	schemaJSON, err := json.MarshalIndent(ResultSchema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("subagent: render result schema: %w", err)
	}

	prompt := fmt.Sprintf(
		"You are a sub-agent. Your response MUST be a single JSON object "+
			"with no additional prose, matching this schema:\n\n"+
			"```json\n%s\n```\n\n"+
			"Respond with the JSON object only. Do not include explanations, "+
			"apologies, or any other text outside the JSON object.",
		string(schemaJSON),
	)

	return systemprompt.New(systemprompt.WithContentFunc(func() string { return prompt }))
}
