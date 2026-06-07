package loop

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertions that *Step satisfies all public interfaces.
var (
	_ TurnRunner    = (*Step)(nil)
	_ TurnSubmitter = (*Step)(nil)
	_ TurnExecutor  = (*Step)(nil)
)

// TestInterfaces_TurnRunner_CallsThrough verifies that calling Turn via the
// TurnRunner interface produces the same result as calling directly on the
// concrete *Step.
func TestInterfaces_TurnRunner_CallsThrough(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	step := New(WithState(mem))
	var runner TurnRunner = step

	prov := &mockProvider{
		artifacts: []artifact.Artifact{artifact.Text{Content: "world"}},
	}

	st, err := runner.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	// Direct call should produce identical results.
	mem2 := &state.Buffer{}
	mem2.Append(state.RoleUser, artifact.Text{Content: "hello"})
	step2 := New(WithState(mem2))
	st2, err := step2.Turn(context.Background(), mem2, prov)
	require.NoError(t, err)

	// Verify turns match without comparing Timestamps (which differ between runs).
	require.Len(t, st.Turns(), 2)
	require.Len(t, st2.Turns(), 2)
	assert.Equal(t, st2.Turns()[0].Role, st.Turns()[0].Role)
	assert.Equal(t, st2.Turns()[1].Role, st.Turns()[1].Role)
	assert.Equal(t, state.RoleAssistant, st.Turns()[1].Role)
}

// TestInterfaces_TurnSubmitter_CallsThrough verifies that calling Submit via
// the TurnSubmitter interface produces the same result as calling directly on
// the concrete *Step.
func TestInterfaces_TurnSubmitter_CallsThrough(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	step := New(WithState(mem))
	var submitter TurnSubmitter = step

	st, err := submitter.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "world"})
	require.NoError(t, err)

	// Direct call should produce identical results.
	mem2 := &state.Buffer{}
	mem2.Append(state.RoleUser, artifact.Text{Content: "hello"})
	step2 := New(WithState(mem2))
	st2, err := step2.Submit(context.Background(), mem2, state.RoleUser, artifact.Text{Content: "world"})
	require.NoError(t, err)

	// Verify turns match without comparing Timestamps (which differ between runs).
	require.Len(t, st.Turns(), 2)
	require.Len(t, st2.Turns(), 2)
	assert.Equal(t, st2.Turns()[0].Role, st.Turns()[0].Role)
	assert.Equal(t, st2.Turns()[1].Role, st.Turns()[1].Role)
	assert.Equal(t, state.RoleUser, st.Turns()[1].Role)
}

// TestInterfaces_TurnExecutor_CallsThrough verifies that calling both Turn
// and Submit via the TurnExecutor interface works correctly and produces the
// same results as direct calls.
func TestInterfaces_TurnExecutor_CallsThrough(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	step := New(WithState(mem))
	var executor TurnExecutor = step

	prov := &mockProvider{
		artifacts: []artifact.Artifact{artifact.Text{Content: "assistant"}},
	}

	st, err := executor.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	st, err = executor.Submit(context.Background(), st, state.RoleUser, artifact.Text{Content: "user"})
	require.NoError(t, err)

	turns := st.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, state.RoleUser, turns[2].Role)
}

// TestInterfaces_TurnExecutor_Embedment verifies that a value of type
// TurnExecutor can be used as both TurnRunner and TurnSubmitter.
func TestInterfaces_TurnExecutor_Embedment(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	step := New(WithState(mem))
	var executor TurnExecutor = step

	// TurnExecutor embeds TurnRunner, so it can be used as TurnRunner.
	var runner TurnRunner = executor
	assert.NotNil(t, runner)

	// TurnExecutor embeds TurnSubmitter, so it can be used as TurnSubmitter.
	var submitter TurnSubmitter = executor
	assert.NotNil(t, submitter)
}
