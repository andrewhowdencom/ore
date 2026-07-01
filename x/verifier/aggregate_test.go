package verifier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunAll_MixedResults(t *testing.T) {
	v1 := &mockVerifier{name: "pass", status: VerificationPass, report: "ok"}
	v2 := &mockVerifier{name: "fail", status: VerificationFail, report: "not ok"}
	v3 := &mockVerifier{name: "error", status: VerificationError, err: errors.New("boom")}

	results := RunAll(context.Background(), []Verifier{v1, v2, v3}, ledger.NewThread())

	require.Len(t, results, 3)
	// Sorted by name.
	assert.Equal(t, "error", results[0].Name)
	assert.Equal(t, VerificationError, results[0].Status)
	assert.Equal(t, "boom", results[0].Report)
	assert.Equal(t, "fail", results[1].Name)
	assert.Equal(t, VerificationFail, results[1].Status)
	assert.Equal(t, "pass", results[2].Name)
	assert.Equal(t, VerificationPass, results[2].Status)
}

func TestRunAll_Empty(t *testing.T) {
	results := RunAll(context.Background(), nil, ledger.NewThread())
	assert.Empty(t, results)
}

func TestRunAll_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	v := &mockVerifier{name: "cancelled", status: VerificationPass}
	results := RunAll(ctx, []Verifier{v}, ledger.NewThread())
	require.Len(t, results, 1)
	// The mock verifier does not check context, so it still returns Pass.
	assert.Equal(t, VerificationPass, results[0].Status)
}

func TestRunAll_ParallelExecution(t *testing.T) {
	v1 := &slowVerifier{name: "slow1", duration: 100 * time.Millisecond}
	v2 := &slowVerifier{name: "slow2", duration: 100 * time.Millisecond}

	start := time.Now()
	results := RunAll(context.Background(), []Verifier{v1, v2}, ledger.NewThread())
	elapsed := time.Since(start)

	require.Len(t, results, 2)
	// If sequential, this would take ~200ms. Parallel should be ~100ms.
	assert.Less(t, elapsed, 150*time.Millisecond, "verifiers should run in parallel")
}

type slowVerifier struct {
	name     string
	duration time.Duration
}

func (s *slowVerifier) Verify(ctx context.Context, st ledger.State) (VerificationResult, error) {
	select {
	case <-time.After(s.duration):
		return VerificationResult{Name: s.name, Status: VerificationPass}, nil
	case <-ctx.Done():
		return VerificationResult{Name: s.name, Status: VerificationError, Report: ctx.Err().Error()}, ctx.Err()
	}
}

func TestBuildReport_Empty(t *testing.T) {
	report := BuildReport(nil)
	assert.Contains(t, report, "# Verification Report")
}

func TestBuildReport_Single(t *testing.T) {
	results := []VerificationResult{
		{Name: "test", Status: VerificationPass, Report: "all good"},
	}
	report := BuildReport(results)
	assert.Contains(t, report, "## test")
	assert.Contains(t, report, "Pass")
	assert.Contains(t, report, "all good")
}

func TestBuildReport_Multiple(t *testing.T) {
	results := []VerificationResult{
		{Name: "a", Status: VerificationPass, Report: "ok"},
		{Name: "b", Status: VerificationFail, Report: "not ok"},
	}
	report := BuildReport(results)
	assert.Contains(t, report, "## a")
	assert.Contains(t, report, "## b")
	assert.Contains(t, report, "Pass")
	assert.Contains(t, report, "Fail")
}

func TestBuildReport_NoReport(t *testing.T) {
	results := []VerificationResult{
		{Name: "empty", Status: VerificationPass, Report: ""},
	}
	report := BuildReport(results)
	assert.Contains(t, report, "## empty")
	assert.Contains(t, report, "Pass")
	assert.NotContains(t, report, "```")
}
