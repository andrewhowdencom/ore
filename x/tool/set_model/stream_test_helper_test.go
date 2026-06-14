package set_model

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/require"
)

// noopProvider is a minimal provider.Provider implementation that satisfies
// the interface but never returns artifacts. Tests construct streams from
// a Manager (which requires a provider) but never invoke the provider
// because they only exercise the slash command's SetMetadata codepath.
type noopProvider struct{}

func (noopProvider) Invoke(_ context.Context, _ state.State, _ chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	return nil
}

// noopTurnProcessor is a TurnProcessor that never runs. SetMetadata does
// not trigger inference, so a real processor is unnecessary; this noop
// exists solely to satisfy NewManager's required parameter.
func noopTurnProcessor(_ context.Context, _ loop.TurnExecutor, st state.State, _ provider.Provider) (state.State, error) {
	return st, nil
}

// newMockStream constructs a real *session.Stream backed by a memory store.
// The slash handler only needs the stream's GetMetadata / SetMetadata
// methods; the manager wiring exists solely to satisfy the constructor's
// type signature.
func newMockStream(t *testing.T) *session.Stream {
	t.Helper()

	store := session.NewMemoryStore()
	prov := noopProvider{}
	mgr := session.NewManager(
		store,
		prov,
		func(*session.Stream) ([]loop.Option, error) { return nil, nil },
		noopTurnProcessor,
	)
	stream, err := mgr.Create()
	require.NoError(t, err)
	require.NotNil(t, stream)
	return stream
}
