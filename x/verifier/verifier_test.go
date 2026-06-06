package verifier

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
)

type mockVerifier struct {
	name   string
	status Status
	report string
	err    error
}

func (m *mockVerifier) Verify(ctx context.Context, st state.State) (VerificationResult, error) {
	return VerificationResult{
		Name:   m.name,
		Status: m.status,
		Report: m.report,
	}, m.err
}

var _ Verifier = (*mockVerifier)(nil)

func TestStatus_String(t *testing.T) {
	assert.Equal(t, "Pass", VerificationPass.String())
	assert.Equal(t, "Fail", VerificationFail.String())
	assert.Equal(t, "Error", VerificationError.String())
	assert.Equal(t, "Unknown", Status(999).String())
}

func TestMockVerifier_Verify(t *testing.T) {
	mv := &mockVerifier{name: "test", status: VerificationPass, report: "ok"}
	res, err := mv.Verify(context.Background(), &state.Buffer{})
	assert.NoError(t, err)
	assert.Equal(t, "test", res.Name)
	assert.Equal(t, VerificationPass, res.Status)
	assert.Equal(t, "ok", res.Report)
}

func TestMockVerifier_Verify_WithError(t *testing.T) {
	wantErr := errors.New("boom")
	mv := &mockVerifier{name: "fail", status: VerificationError, err: wantErr}
	res, err := mv.Verify(context.Background(), &state.Buffer{})
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, "fail", res.Name)
	assert.Equal(t, VerificationError, res.Status)
}
