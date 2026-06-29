package cognitive

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider satisfies provider.Provider with no-op behavior.
// The SingleShot pattern does not call Invoke directly (the underlying
// step does), so a stub is sufficient for the unit tests.
type stubProvider struct{}

var _ provider.Provider = (*stubProvider)(nil)

func (s *stubProvider) Invoke(_ context.Context, _ ledger.State, _ models.Spec, _ chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	return nil
}

// mockTurnRunner is a test double implementing loop.TurnRunner.
// It counts invocations and returns the configured state and error.
type mockTurnRunner struct {
	calls    int32
	lastSt   ledger.State
	lastSpec models.Spec
	lastProv provider.Provider
	ret      ledger.State
	err      error
}

var _ loop.TurnRunner = (*mockTurnRunner)(nil)

func (m *mockTurnRunner) Turn(_ context.Context, st ledger.State, spec models.Spec, p provider.Provider, _ ...provider.InvokeOption) (ledger.State, error) {
	atomic.AddInt32(&m.calls, 1)
	m.lastSt = st
	m.lastSpec = spec
	m.lastProv = p
	return m.ret, m.err
}

func TestSingleShot_Name(t *testing.T) {
	assert.Equal(t, "single_shot", (&SingleShot{}).Name())
}

func TestSingleShot_Run(t *testing.T) {
	tests := []struct {
		name      string
		ret       ledger.State
		err       error
		wantCalls int32
		wantErr   bool
	}{
		{
			name:      "returns state from step",
			ret:       &ledger.Buffer{},
			wantCalls: 1,
		},
		{
			name:      "propagates step error",
			err:       errors.New("step error"),
			wantCalls: 1,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockTurnRunner{ret: tt.ret, err: tt.err}
			pat := &SingleShot{
				Step:     mock,
				Provider: &stubProvider{},
				Spec:     models.Spec{Name: "test"},
			}
			st := &ledger.Buffer{}
			result, err := pat.Run(context.Background(), st)
			require.Equal(t, tt.wantCalls, atomic.LoadInt32(&mock.calls))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Same(t, st, mock.lastSt)
			assert.Same(t, pat.Provider, mock.lastProv)
			assert.Equal(t, models.Spec{Name: "test"}, mock.lastSpec)
			assert.Same(t, tt.ret, result)
		})
	}
}

func TestSingleShot_Run_ForwardsAllArgs(t *testing.T) {
	mock := &mockTurnRunner{ret: &ledger.Buffer{}}
	pat := &SingleShot{
		Step:     mock,
		Provider: &stubProvider{},
		Spec:     models.Spec{Name: "specific", Window: 100, MaxOutputTokens: 200},
	}
	st := &ledger.Buffer{}
	_, err := pat.Run(context.Background(), st)
	require.NoError(t, err)
	assert.Equal(t, models.Spec{Name: "specific", Window: 100, MaxOutputTokens: 200}, mock.lastSpec)
	assert.Same(t, pat.Provider, mock.lastProv)
	assert.Same(t, st, mock.lastSt)
}
