// Package compaction provides a state compaction framework that reduces the
// size of a conversation state.
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
// SummarizeStrategy is a strategy that calls an LLM provider to summarize
// conversation history, replacing all turns with a single synthetic system
// summary turn. It always summarizes the entire history; no turns are
// preserved verbatim after compaction.
//
// The provider is called with the full history loaded into a temporary
// state.Buffer, followed by a user prompt asking for a concise summary. The
// summary turn uses RoleSystem because it is injected context about prior
// conversation, not a real assistant response.
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
//		compaction.WithTrigger(compaction.TokenUsageTrigger{MaxTokens: 8000}),
//		compaction.WithStrategy(compaction.SummarizeStrategy{Provider: prov}),
//	)
//
// WithStrategy accumulates; each call appends another strategy to the
// pipeline. Strategies execute in registration order.
//
//	for {
//		turns, didCompact, err := compactor.MaybeCompact(ctx, buf.Turns())
//		if err != nil {
//			// handle error
//		}
//		if didCompact {
//			buf.LoadTurns(turns)
//		}
//		_, err = step.Turn(ctx, buf, provider)
//	}
//
// The compactor does not emit events. If an application needs to log
// compaction events, it should do so at the call site based on the bool
// return value of MaybeCompact.
//
// # Defensive composition
//
// Applications should protect against provider failures and context overflow
// by setting trigger thresholds with safety margins. For example, set
// MaxTokens well below the provider's hard limit.
//
// Compaction must be called from the same goroutine as step.Turn().
// state.Buffer is not safe for concurrent use.
package compaction
