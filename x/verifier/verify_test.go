package verifier

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPattern struct {
	returnState ledger.State
	err         error
	callCount   int
}

func (m *mockPattern) Run(ctx context.Context, st ledger.State) (ledger.State, error) {
	m.callCount++
	return m.returnState, m.err
}

func (m *mockPattern) Name() string { return "mock" }

var _ Pattern = (*mockPattern)(nil)

type failOnceVerifier struct {
	called bool
}

func (f *failOnceVerifier) Verify(ctx context.Context, st ledger.State) (VerificationResult, error) {
	if !f.called {
		f.called = true
		return VerificationResult{Name: "fail-once", Status: VerificationFail, Report: "fail"}, nil
	}
	return VerificationResult{Name: "fail-once", Status: VerificationPass, Report: "pass"}, nil
}

var _ Verifier = (*failOnceVerifier)(nil)

func TestWithVerification_PassOnFirstTry(t *testing.T) {
	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v := &mockVerifier{name: "pass", status: VerificationPass}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v))

	result, err := pattern.Run(context.Background(), mem)
	require.NoError(t, err)
	assert.Equal(t, 1, mock.callCount)
	assert.Equal(t, mem, result)

	// State should have only the user turn (no system turn injected).
	turns := result.Turns()
	require.Len(t, turns, 1)
	assert.Equal(t, ledger.RoleUser, turns[0].Role)
}

func TestWithVerification_FailThenRetryThenPass(t *testing.T) {
	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

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
	assert.Equal(t, ledger.RoleUser, turns[0].Role)
	assert.Equal(t, ledger.RoleSystem, turns[1].Role)
	assert.Equal(t, "text", turns[1].Artifacts[0].Kind())
	assert.Contains(t, turns[1].Artifacts[0].(artifact.Text).Content, "Verification Report")
}

func TestWithVerification_MaxRetriesExceeded(t *testing.T) {
	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v := &mockVerifier{name: "always-fail", status: VerificationFail, report: "failed"}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v), WithMaxRetries(1))

	_, err := pattern.Run(context.Background(), mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed after 1 retries")
	assert.Equal(t, 2, mock.callCount) // initial + 1 retry
}

func TestWithVerification_VerifierError(t *testing.T) {
	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v := &mockVerifier{name: "error", status: VerificationError, err: errors.New("boom")}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v))

	_, err := pattern.Run(context.Background(), mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification error")
	assert.Equal(t, 1, mock.callCount)
}

func TestWithVerification_InnerPatternError(t *testing.T) {
	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

	wantErr := errors.New("inner failed")
	mock := &mockPattern{returnState: mem, err: wantErr}
	v := &mockVerifier{name: "pass", status: VerificationPass}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v))

	_, err := pattern.Run(context.Background(), mem)
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, 1, mock.callCount)
}

func TestWithVerification_MultipleVerifiers(t *testing.T) {
	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v1 := &mockVerifier{name: "pass", status: VerificationPass}
	v2 := &mockVerifier{name: "fail", status: VerificationFail, report: "failing"}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v1, v2))

	_, err := pattern.Run(context.Background(), mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed")
	assert.Equal(t, 4, mock.callCount) // initial + 3 retries (default)
}

func TestWithVerification_DefaultMaxRetries(t *testing.T) {
	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

	mock := &mockPattern{returnState: mem}
	v := &mockVerifier{name: "always-fail", status: VerificationFail}

	step := loop.New(loop.WithState(mem))
	pattern := WithVerification(mock, step, WithVerifiers(v))

	_, err := pattern.Run(context.Background(), mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed after 3 retries")
	assert.Equal(t, 4, mock.callCount) // initial + 3 retries
}

func TestWithVerification_Name(t *testing.T) {
	mock := &mockPattern{returnState: ledger.NewThread()}
	step := loop.New(loop.WithState(ledger.NewThread()))
	pattern := WithVerification(mock, step)

	assert.Equal(t, "verified", pattern.Name())
}