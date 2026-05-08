// Package core implements the minimal inference primitive: a single turn
// orchestrator that delegates all LLM-specific work to a provider adapter.
package core

import (
	"context"

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
func (l *Loop) Turn(ctx context.Context, s state.State, p provider.Provider) (state.State, error) {
	artifacts, err := p.Invoke(ctx, s)
	if err != nil {
		return s, err
	}
	s.Append(state.RoleAssistant, artifacts...)
	return s, nil
}
