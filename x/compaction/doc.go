// Package compaction provides a state compaction framework that reduces the
// size of a conversation state to fit within a provider's context window.
//
// Compaction is destructive: it mutates the canonical state.Buffer via
// LoadTurns. The session.Store persists the compacted state. This is
// intentional — the compactor is a state reducer, not a lens. Triggers
// evaluate the canonical state, not a growing shadow history.
//
// The package defines two extension points:
//
//   - Trigger decides whether compaction should run.
//   - Strategy decides how to reduce the turn slice.
//
// Default implementations are provider-agnostic and have zero external
// dependencies. Token-aware triggers and LLM summarization strategies are
// also provided.
//
// # Built-in Triggers
//
// TurnCountTrigger fires when the number of turns exceeds a threshold.
// TokenUsageTrigger inspects the most recent artifact.Usage in the turn
// slice and fires when Usage.TotalTokens exceeds MaxTokens. If no Usage
// artifact is found, it returns false (graceful degradation). This trigger
// is provider-specific because not all providers emit Usage artifacts.
//
// # Built-in Strategies
//
// KeepLastN drops all but the last N turns. It is provider-agnostic and
// fast, making it suitable as a safety margin before more expensive
// strategies.
//
// SummarizeStrategy calls an LLM provider to summarize conversation history,
// replacing dropped turns with a single synthetic system summary turn.
// The summary turn uses RoleSystem because it is injected context about
// prior conversation, not a real assistant response.
//
// SummarizeStrategy only collects artifact.Text responses from the provider.
// Other artifact types (Usage, Reasoning, ToolCall, etc.) are silently
// ignored. This is an MVP limitation; future work may add custom formatters
// or multi-modal support.
//
// # Application wiring
//
// The compactor is called by the application before step.Turn(). If
// compaction occurs, the application must call buf.LoadTurns():
//
//	compactor := compaction.New(
//	    compaction.WithTrigger(compaction.TurnCountTrigger{N: 20}),
//	    compaction.WithStrategy(compaction.KeepLastN{N: 10}),
//	    compaction.WithStrategy(compaction.SummarizeStrategy{Provider: prov, PreserveLastN: 2}),
//	)
//
// WithStrategy accumulates; each call appends another strategy to the
// pipeline. Strategies execute in registration order.
//
//	for {
//	    turns, didCompact, err := compactor.MaybeCompact(ctx, buf.Turns())
//	    if err != nil {
//	        // handle error
//	    }
//	    if didCompact {
//	        buf.LoadTurns(turns)
//	    }
//	    _, err = step.Turn(ctx, buf, provider)
//	}
//
// The compactor does not emit events. If an application needs to log
// compaction events, it should do so at the call site based on the bool
// return value of MaybeCompact.
//
// # Defensive composition
//
// Applications should protect against provider failures and context overflow
// by chaining strategies or setting trigger thresholds with safety margins.
// For example, keep the last N turns before summarizing, or set MaxTokens
// well below the provider's hard limit.
//
// Compaction must be called from the same goroutine as step.Turn().
// state.Buffer is not safe for concurrent use.
package compaction
