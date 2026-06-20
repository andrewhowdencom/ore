package models

// Spec is a value object combining a model identity with its inference
// configuration. It is the canonical argument to provider invocations.
//
// Identity
//
// A Spec's Name is the model identifier as understood by the provider
// that serves it. The same Name may be portable across providers (e.g.
// "claude-opus-4-5" served by Anthropic, Vertex, or OpenRouter) —
// the framework does not enforce a 1:1 mapping; that is the
// application's wiring concern.
//
// Inference configuration
//
// The fields below constrain or influence how the model runs. Pointer
// fields distinguish "explicitly set" from "use the framework / model
// default"; zero-value fields are framework defaults where applicable
// (MaxOutputTokens == 0 means "no opinion" — adapters omit the wire
// field and let their default apply). Adapters that do not recognize a
// given field leave it on the floor; the Spec is permissive by design.
//
// Not in Spec
//
// The following are *data within the inference call*, not inference
// configuration, and live elsewhere (per-call InvokeOptions or
// arguments):
//
//   - System prompt
//   - Tools (function definitions available to the model)
//   - Message history (in state.State)
//   - User-visible content
//
// Capabilities (audio, video, image, tool support) are a separate
// axis and are out of scope for this version. When a concrete
// consumer emerges, add a Capabilities() method on Spec.
type Spec struct {
	// Name is the model identifier as understood by the provider.
	Name string

	// Window is the model's context window size in tokens. Not a
	// request budget; this is the upper bound on input the model
	// can accept. Applications that implement automatic
	// compaction triggers may read this field to decide when to
	// invoke compaction.Summarize; the compaction package itself
	// does not consult it (compaction today is explicit-only via
	// /compact).
	Window int

	// MaxOutputTokens is the per-invocation output token budget.
	// Zero or negative values mean "no opinion" — adapters omit
	// the wire field and use their own default.
	MaxOutputTokens int64

	// Temperature sets the sampling temperature. nil means "use
	// the model's default" (typically 1.0 for most models). 0.0
	// (when not nil) requests greedy decoding.
	Temperature *float64

	// ThinkingLevel asks the model to spend the given amount of
	// reasoning effort. Adapters translate the level to their
	// wire format. The empty value is treated as "off" by
	// adapters; the framework does not normalize the field.
	ThinkingLevel ThinkingLevel

	// TopP is the nucleus sampling cutoff. nil means "use the
	// model's default".
	TopP *float64

	// TopK restricts sampling to the top-K tokens. nil means
	// "use the model's default".
	TopK *int

	// Seed pins the random seed for reproducible outputs. nil
	// means "no seed" (non-deterministic).
	Seed *int64

	// StopSequences are strings at which the model halts
	// generation. nil or empty means "use the model's default".
	StopSequences []string

	// FrequencyPenalty discourages token repetition based on
	// frequency. nil means "use the model's default".
	FrequencyPenalty *float64

	// PresencePenalty discourages token repetition based on
	// presence. nil means "use the model's default".
	PresencePenalty *float64
}
