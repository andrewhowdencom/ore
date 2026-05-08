// Package orchestrate defines orchestration strategies that drive the tack
// framework's inference loop. An Orchestrator receives events from a Surface,
// manages conversation state, and decides when to execute additional turns.
package orchestrate

import "context"

// Orchestrator is the high-level strategy interface. Concrete
// implementations (e.g., ReAct, single-turn) read events from a Surface
// and manage the conversation lifecycle.
type Orchestrator interface {
	// Run blocks until the context is cancelled or the surface's event
	// channel is closed. It processes user events and drives the
	// conversation according to the orchestration strategy.
	Run(ctx context.Context) error
}
