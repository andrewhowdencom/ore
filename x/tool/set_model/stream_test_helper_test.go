package set_model

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/require"
)

// noopProvider is a minimal provider.Provider implementation that satisfies
// the interface but never returns artifacts. Tests construct streams from
// a Manager (which requires a provider) but never invoke the provider
// because they only exercise the slash command's SetMetadata codepath.
type noopProvider struct{}

func (noopProvider) Invoke(_ context.Context, _ ledger.State, _ models.Spec, _ chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	return nil
}

// noopTurnProcessor is a TurnProcessor that never runs. SetMetadata does
// not trigger inference, so a real processor is unnecessary; this noop
// exists solely to satisfy NewManager's required parameter.
func noopTurnProcessor(_ context.Context, _ *loop.Step, st ledger.State, _ provider.Provider, _ models.Spec) (ledger.State, error) {
	return st, nil
}

// newMockStream constructs a real *junk.Stream backed by a memory store.
// The slash handler only needs the stream's GetMetadata / SetMetadata
// methods; the manager wiring exists solely to satisfy the constructor's
// type signature.
func newMockStream(t *testing.T) *junk.Stream {
	t.Helper()

	store := junk.NewMemoryStore()
	prov := noopProvider{}
	mgr := junk.NewManager(
		store,
		prov,
		func(*junk.Stream) ([]loop.Option, error) { return nil, nil },
		noopTurnProcessor,
	)
	stream, err := mgr.Create()
	require.NoError(t, err)
	require.NotNil(t, stream)
	return stream
}
