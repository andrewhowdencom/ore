package cognitive

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/verifier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPattern struct {
	returnState state.State
	err         error
	callCount   int
}

func (m *mockPattern) Run(ctx context.Context, st state.State) (state.State, error) {
	m.callCount++
	return m.returnState, m.err
}

var _ Pattern = (*mockPattern)(nil)

type mockVerifier struct {
	name   string
	status verifier.Status
	report string
	err    error
}

func (m *mockVerifier) Verify(ctx context.Context, st state.State) (verifier.VerificationResult, error) {
	return verifier.VerificationResult{
		Name:   m.name,
		Status: m.status,
		Report: m.report,
	}, m.err
}

var _ verifier.Verifier = (*mockVerifier)(nil)

type failOnceVerifier struct {
	called bool
}

func (f *failOnceVerifier) Verify(ctx context.Context, st state.State) (verifier.VerificationResult, error) {
	if !f.called {
		f.called = true
		return verifier.VerificationResult{Name: "fail-once", Status: verifier.VerificationFail, Report: "fail"}, nil
	}
	return verifier.VerificationResult{Name: "fail-once", Status: verifier.VerificationPass, Report: "pass"}, nil
}

var _ verifier.Verifier = (*failOnceVerifier)(nil)

func TestWithVerification_PassOnFirstTry(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v := &mockVerifier{name: "pass", status: verifier.VerificationPass}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v))

	result, err := pattern.Run(context.Background(), mem)
	require.NoError(t, err)
	assert.Equal(t, 1, mock.callCount)
	assert.Equal(t, mem, result)

	// State should have only the user turn (no system turn injected).
	turns := result.Turns()
	require.Len(t, turns, 1)
	assert.Equal(t, state.RoleUser, turns[0].Role)
}

func TestWithVerification_FailThenRetryThenPass(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	failOnce := &failOnceVerifier{}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(failOnce), WithMaxRetries(1))

	result, err := pattern.Run(context.Background(), mem)
	require.NoError(t, err)
	assert.Equal(t, 2, mock.callCount)

	// State should have user + system turn (verification report).
	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleSystem, turns[1].Role)
	assert.Equal(t, "text", turns[1].Artifacts[0].Kind())
	assert.Contains(t, turns[1].Artifacts[0].(artifact.Text).Content, "Verification Report")
}

func TestWithVerification_MaxRetriesExceeded(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v := &mockVerifier{name: "always-fail", status: verifier.VerificationFail, report: "failed"}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v), WithMaxRetries(1))

	_, err := pattern.Run(context.Background(), mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed after 1 retries")
	assert.Equal(t, 2, mock.callCount) // initial + 1 retry
}

func TestWithVerification_VerifierError(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v := &mockVerifier{name: "error", status: verifier.VerificationError, err: errors.New("boom")}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v))

	_, err := pattern.Run(context.Background(), mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification error")
	assert.Equal(t, 1, mock.callCount)
}

func TestWithVerification_InnerPatternError(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	wantErr := errors.New("inner failed")
	mock := &mockPattern{returnState: mem, err: wantErr}
	v := &mockVerifier{name: "pass", status: verifier.VerificationPass}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v))

	_, err := pattern.Run(context.Background(), mem)
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, 1, mock.callCount)
}

func TestWithVerification_MultipleVerifiers(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v1 := &mockVerifier{name: "pass", status: verifier.VerificationPass}
	v2 := &mockVerifier{name: "fail", status: verifier.VerificationFail, report: "failing"}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v1, v2))

	_, err := pattern.Run(context.Background(), mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed")
	assert.Equal(t, 4, mock.callCount) // initial + 3 retries (default)
}

func TestWithVerification_DefaultMaxRetries(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v := &mockVerifier{name: "always-fail", status: verifier.VerificationFail}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v))

	_, err := pattern.Run(context.Background(), mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed after 3 retries")
	assert.Equal(t, 4, mock.callCount) // initial + 3 retries
}
