package verifier

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNoShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell tests skipped on Windows")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
}

func TestExecVerifier_Pass(t *testing.T) {
	skipIfNoShell(t)
	ev := &ExecVerifier{
		Name:    "pass",
		Command: "sh",
		Args:    []string{"-c", "echo hello"},
	}
	res, err := ev.Verify(context.Background(), &state.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, "pass", res.Name)
	assert.Equal(t, VerificationPass, res.Status)
	assert.Contains(t, res.Report, "hello")
}

func TestExecVerifier_Fail(t *testing.T) {
	skipIfNoShell(t)
	ev := &ExecVerifier{
		Name:    "fail",
		Command: "sh",
		Args:    []string{"-c", "echo error-output && exit 1"},
	}
	res, err := ev.Verify(context.Background(), &state.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, "fail", res.Name)
	assert.Equal(t, VerificationFail, res.Status)
	assert.Contains(t, res.Report, "error-output")
}

func TestExecVerifier_Error(t *testing.T) {
	ev := &ExecVerifier{
		Name:    "error",
		Command: "nonexistent_command_that_should_not_exist_12345",
	}
	res, err := ev.Verify(context.Background(), &state.Buffer{})
	require.Error(t, err)
	assert.Equal(t, "error", res.Name)
	assert.Equal(t, VerificationError, res.Status)
}

func TestExecVerifier_Timeout(t *testing.T) {
	skipIfNoShell(t)
	// Use sleep directly (not via sh) so CommandContext can kill it reliably.
	ev := &ExecVerifier{
		Name:    "timeout",
		Command: "sleep",
		Args:    []string{"2"},
		Timeout: 100 * time.Millisecond,
	}
	res, err := ev.Verify(context.Background(), &state.Buffer{})
	require.Error(t, err)
	assert.Equal(t, "timeout", res.Name)
	assert.Equal(t, VerificationError, res.Status)
}

func TestExecVerifier_Dir(t *testing.T) {
	skipIfNoShell(t)
	ev := &ExecVerifier{
		Name:    "dir",
		Command: "sh",
		Args:    []string{"-c", "pwd"},
		Dir:     "/tmp",
	}
	res, err := ev.Verify(context.Background(), &state.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, VerificationPass, res.Status)
	assert.Contains(t, res.Report, "/tmp")
}
