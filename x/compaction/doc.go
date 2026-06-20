// Package compaction provides a state-buffer compaction framework for
// ore conversations. The design is non-destructive: a compaction is an
// in-band event recorded in the buffer, not a destructive rewrite of
// state.
//
// # Design
//
// Compaction in ore is *append-only* and *cumulative*:
//
//   - The state buffer grows monotonically. Compaction never removes
//     turns from the canonical buffer.
//   - A compaction produces a single RoleSystem turn carrying an
//     artifact.Compaction (structured metadata) and an artifact.Text
//     (the LLM-facing summary). It is appended to the buffer by the
//     caller (e.g. a slash handler invoking /compact).
//   - The LLM-facing view is projected through the latest
//     artifact.Compaction via the Transform in this package. The
//     summary stands in for everything older than itself; multiple
//     compactions are cumulative, with each summary absorbing
//     everything that preceded it.
//   - Analytics consumers walk the raw buffer unchanged. Pre-compaction
//     turns remain in the buffer so attribution to specific tools and
//     artifacts survives compaction.
//
// This replaces the previous destructive model (Compactor + Trigger +
// Strategy) in which MaybeCompact returned a replacement turn slice
// for the caller to LoadTurns into the buffer. The destructive
// surface has been removed entirely; there is no opt-in path for it.
// AGENTS.md endorses aggressive refactoring at this stage of the
// project.
//
// # Components
//
//   - Summarize: a package-level function that calls an LLM provider
//     to produce a single RoleSystem compaction turn. The function
//     returns (state.Turn{}, ErrTruncatedSummary) on truncation; the
//     caller is expected to NOT append anything to the buffer in
//     that case.
//
//   - Transform: a loop.Transform that scans the buffer for the
//     latest artifact.Compaction and returns a state.View exposing
//     only the compaction and subsequent turns. It is stateless and
//     goroutine-safe; a single instance may be shared across many
//     Step configurations.
//
//   - artifact.Compaction: the metadata artifact that marks a
//     compaction turn. Defined in the root artifact/ package because
//     it is a framework primitive.
//
// # Explicit invocation
//
// Compaction is triggered explicitly by the user invoking /compact,
// not by an automatic trigger (no token-count watcher, no turn-count
// watcher). The slash handler in the calling application is
// responsible for invoking Summarize and appending the result via
// session.Stream.AppendTurn. Future work may introduce a Trigger
// interface as a separate package if applications want automatic
// compaction; today, the responsibility lives with the caller.
//
// # Truncation contract
//
// Summarize reads the provider's final artifact.StopReason. If the
// reason is StopReasonLength, the function returns the zero
// state.Turn and an error wrapping ErrTruncatedSummary. This
// replaces the previous silent-corruption behavior in which a
// truncated summary (often a one-token '##' fragment) was written
// into the conversation buffer as if it were valid. The contract is
// now:
//
//	turn, err := compaction.Summarize(ctx, prov, spec, stream.Turns())
//	if errors.Is(err, compaction.ErrTruncatedSummary) {
//	    // The model hit its output cap mid-summary. Do NOT append
//	    // anything; surface the failure to the user.
//	}
//	if err != nil {
//	    return err
//	}
//	if err := stream.AppendTurn(ctx, turn.Role, turn.Artifacts...); err != nil {
//	    return err
//	}
//
// # LLM-facing projection
//
// The Transform is intended to be registered alongside other
// transforms (systemprompt, guardrails) in the step's transform
// chain. Registration order matters: system prompts that should
// appear before the summary should be registered before the
// compaction transform; system prompts that should override the
// summary context should be registered after.
//
// Example:
//
//	step := loop.New(
//	    loop.WithTransforms(
//	        systemprompt.New(...),    // prepends the system persona
//	        compaction.NewTransform(), // projects through latest compaction
//	        guardrails.New(...),      // adds safety rules on top
//	    ),
//	    ...
//	)
//
// The canonical buffer is not affected by any of these transforms;
// they compose purely on the LLM-facing view assembled per call.
//
// # Per-invocation budget
//
// Summarize owns its own output budget via models.Spec.MaxOutputTokens
// (default 8192). The function passes this to the provider as a
// per-invocation provider.WithMaxTokens option so the model has room
// to produce a complete summary regardless of the adapter's per-model
// default. This is the fix for the historical 'compaction returns ##'
// bug, which was caused by an adapter-level default of 1 token
// combined with a strategy that did not pass any invoke options.
//
// # Threading
//
// Summarize and Transform are goroutine-safe. They do not share
// mutable state. The state.Buffer itself is not goroutine-safe, as
// documented in package state; compaction must be called from the
// same goroutine as the buffer's owner (typically the session's
// worker goroutine).
package compaction
