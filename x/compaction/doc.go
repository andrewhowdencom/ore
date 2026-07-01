// Package compaction provides a state-buffer compaction framework for
// ore conversations. The design is non-destructive: a compaction is an
// in-band event recorded in the buffer, not a destructive rewrite of
// ledger.
//
// # Design
//
// Compaction in ore is *append-only* and *cumulative*:
//
//   - The state buffer grows monotonically. Compaction never removes
//     turns from the canonical buffer.
//   - A compaction produces a single RoleSystem turn carrying only
//     an artifact.Text (the LLM-facing summary), and a companion
//     BoundaryInfo struct (the structured provenance of the
//     compaction event). The turn is appended to the buffer by the
//     caller; the BoundaryInfo is written to ledger.Meta under the
//     ore.compaction.boundary.* keys (see [MetaKeyBoundaryIndex] and
//     [MetaKeyBoundaryInfo]) via stream.MarkBoundary or an equivalent
//     helper.
//   - The LLM-facing view is projected through the boundary recorded
//     in ledger.Meta via the Transform in this package. The summary
//     stands in for everything older than itself; multiple
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
//   - Summarize: a package-level function that runs a caller-supplied
//     agent.Agent (configured with a cognitive.SingleShot pattern)
//     to produce a single RoleSystem compaction turn carrying only
//     artifact.Text, plus a BoundaryInfo describing the event. The
//     function returns (ledger.Turn{}, BoundaryInfo{}, ErrTruncatedSummary)
//     on truncation; the caller is expected to NOT append anything
//     to the buffer in that case.
//
//   - Transform: a loop.Transform that reads the boundary index from
//     ledger.Meta and returns a ledger.View exposing only the
//     boundary turn and subsequent turns. It is stateless and
//     goroutine-safe; a single instance may be shared across many
//     Step configurations.
//
//   - BoundaryInfo: the structured provenance of a compaction
//     event — turn count, byte estimate, strategy, model, timestamp.
//     Lives in this package because it is a compaction-specific
//     concern; the state package remains agnostic.
//
// # Why the boundary is in ledger.Meta, not the artifact stream
//
// The artifact stream ([ledger.Turn.Artifacts]) is a journal of what
// was produced in a conversation. Every artifact kind that lives
// there describes something produced by its turn — content
// (artifact.Text, artifact.ToolCall, …), in-flight fragments
// (artifact.TextDelta, …), or per-turn/per-artifact metadata
// (artifact.Usage, artifact.StopReason, artifact.ReasoningSignature).
//
// A compaction boundary does not describe a turn; it describes a
// fact about the buffer as a whole. Smuggling it through the
// artifact stream forced every consumer (the Anthropic wire's
// onlyText predicate, the TUI, the session serializer) to either
// know about the boundary, tolerate it, or silently fail when
// encountering it. The boundary now lives in ledger.Meta — a
// generic metadata channel added to ledger.State for state-level
// facts (compaction boundaries, future checkpoint markers) that
// are not turn-level artifacts.
//
// # Explicit invocation
//
// Compaction is triggered explicitly by the user invoking /compact,
// not by an automatic trigger (no token-count watcher, no turn-count
// watcher). The slash handler in the calling application is
// responsible for building the compactor agent, invoking Summarize,
// appending the summary turn via junk.Stream.AppendTurn, and
// recording the boundary via stream.MarkBoundary. Future work may
// introduce a Trigger interface as a separate package if applications
// want automatic compaction; today, the responsibility lives with
// the caller.
//
// # Truncation contract
//
// Summarize reads the agent's produced turn for artifact.StopReason.
// If the reason is StopReasonLength, the function returns the zero
// ledger.Turn, the zero BoundaryInfo, and an error wrapping
// ErrTruncatedSummary. This replaces the previous silent-corruption
// behavior in which a truncated summary (often a one-token '##'
// fragment) was written into the conversation buffer as if it were
// valid. The contract is now:
//
//	compactAgent := agent.New("compactor",
//	    agent.WithProvider(prov),
//	    agent.WithSpec(spec),
//	    agent.WithPattern(&cognitive.SingleShot{}),
//	)
//	defer compactAgent.Close()
//	turn, info, err := compaction.Summarize(ctx, compactAgent, stream.Turns())
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
//	if err := stream.MarkBoundary(len(stream.Turns())-1, info); err != nil {
//	    return err
//	}
//
// # LLM-facing projection
//
// The Transform is intended to be registered alongside other
// transforms (systemprompt, guardrails) in the agent's transform
// chain. Registration order matters: system prompts that should
// appear before the summary should be registered before the
// compaction transform; system prompts that should override the
// summary context should be registered after.
//
// Example (composing transforms on the main conversation step; the
// compactor's own agent does not need the system-prompt transform
// unless the summarization model requires it):
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
// The compactor's caller controls the summarization output budget via
// models.Spec.MaxOutputTokens on the compactor agent (set via
// agent.WithSpec). The agent's step forwards the spec to the
// provider, which translates MaxOutputTokens into the wire format.
//
// There is no internal default. Callers that want an explicit budget
// (the recommended path — too-small caps reproduce the historical
// 'compaction returns ##' bug) set the spec.MaxOutputTokens field on
// the compactor agent. A common value is 8192 tokens, which is
// enough for the standard five-section handoff summary and stays
// within the long-tail of supported model output caps.
//
// # Threading
//
// Summarize and Transform are goroutine-safe. They do not share
// mutable ledger. The ledger.Thread itself is not goroutine-safe, as
// documented in package state; compaction must be called from the
// same goroutine as the buffer's owner (typically the session's
// worker goroutine).
//
// Summarize subscribes to the compactor agent's "turn_complete"
// event and uses a goroutine to capture the produced turn. The
// goroutine exits after the first matching event; the event channel
// is left unread for the remainder of the agent's lifetime but is
// closed when the caller closes the agent (typically via a
// defer).
package compaction