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
// dependencies. Token-aware triggers and LLM summarization strategies can
// be plugged in by implementing the Trigger and Strategy interfaces.
//
// # Application wiring
//
// The compactor is called by the application before step.Turn(). If
// compaction occurs, the application must call buf.LoadTurns():
//
//	compactor := compaction.New(
//	    compaction.WithTrigger(compaction.TurnCountTrigger{N: 20}),
//	    compaction.WithStrategy(compaction.KeepLastN{N: 10}),
//	)
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
// Compaction must be called from the same goroutine as step.Turn().
// state.Buffer is not safe for concurrent use.
package compaction
