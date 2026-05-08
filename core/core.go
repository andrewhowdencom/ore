// Package core implements the minimal inference primitive: a single turn
// orchestrator that delegates all LLM-specific work to a provider adapter.
package core

import (
	"context"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
)

// Loop is the minimal core inference primitive. It orchestrates a single
// turn by delegating to a provider adapter and appending the response to state.
type Loop struct{}

// Turn executes a single inference turn:
//   1. Invoke the provider with the current state.
//   2. Append the returned artifacts to state with RoleAssistant.
//   3. Return the mutated state.
//
// An optional delta channel may be passed (via variadic) to enable streaming.
// If the channel is non-nil and the provider implements StreamingProvider,
// deltas are emitted to the channel in real-time. Otherwise, the call falls
// back to the standard Invoke path. Existing callers compile without changes.
func (l *Loop) Turn(ctx context.Context, s state.State, p provider.Provider, deltasCh ...chan<- artifact.Artifact) (state.State, error) {
	var ch chan<- artifact.Artifact
	if len(deltasCh) > 0 {
		ch = deltasCh[0]
	}

	var artifacts []artifact.Artifact
	var err error

	if ch != nil {
		if sp, ok := p.(provider.StreamingProvider); ok {
			artifacts, err = sp.InvokeStreaming(ctx, s, ch)
		} else {
			artifacts, err = p.Invoke(ctx, s)
		}
	} else {
		artifacts, err = p.Invoke(ctx, s)
	}

	if err != nil {
		return s, err
	}
	s.Append(state.RoleAssistant, artifacts...)
	return s, nil
}
