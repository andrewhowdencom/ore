package loop

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
)

// TurnRunner runs a single inference turn with a provider.
// Implementations invoke the provider, accumulate artifacts, and emit
// streaming events. The returned state is the conversation state for the next
// turn, excluding any inference-only views. The canonical implementation is
// Step.Turn.
type TurnRunner interface {
	Turn(ctx context.Context, st ledger.State, spec models.Spec, p provider.Provider, opts ...provider.InvokeOption) (ledger.State, error)
}

// TurnSubmitter records a non-inference turn into state and emits a
// TurnCompleteEvent. The canonical implementation is Step.Submit.
type TurnSubmitter interface {
	Submit(ctx context.Context, st ledger.State, role ledger.Role, artifacts ...artifact.Artifact) (ledger.State, error)
}

// TurnExecutor combines TurnRunner and TurnSubmitter into a single
// interface for cognitive patterns that need both capabilities.
// Step satisfies this interface via delegation.
type TurnExecutor interface {
	TurnRunner
	TurnSubmitter
}
