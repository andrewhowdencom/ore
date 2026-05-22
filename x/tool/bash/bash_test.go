package bash

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBash_Echo(t *testing.T) {
	t.Parallel()
	result, err := Bash(context.Background(), map[string]any{"command": "echo hello"})
	require.NoError(t, err)

	m := result.(map[string]any)
	assert.Equal(t, "hello\n", m["stdout"])
	assert.Equal(t, "", m["stderr"])
	assert.Equal(t, 0, m["exit_code"])
}

func TestBash_InvalidCommand(t *testing.T) {
	t.Parallel()
	result, err := Bash(context.Background(), map[string]any{"command": "exit 42"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited with code 42")

	m := result.(map[string]any)
	assert.Equal(t, "", m["stdout"])
	assert.Equal(t, "", m["stderr"])
	assert.Equal(t, 42, m["exit_code"])
}

func TestBash_WorkingDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	result, err := Bash(context.Background(), map[string]any{
		"command":           "pwd",
		"working_directory": dir,
	})
	require.NoError(t, err)

	m := result.(map[string]any)
	assert.Contains(t, m["stdout"], filepath.Base(dir))
}

func TestBash_Timeout(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep command not available")
	}

	start := time.Now()
	_, err := Bash(context.Background(), map[string]any{
		"command":         "sleep 10",
		"timeout_seconds": 1,
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Less(t, elapsed, 3*time.Second)
}

func TestBash_EmptyCommand(t *testing.T) {
	t.Parallel()
	_, err := Bash(context.Background(), map[string]any{"command": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestBash_StderrCapture(t *testing.T) {
	t.Parallel()
	result, err := Bash(context.Background(), map[string]any{"command": "echo error >&2; exit 1"})
	require.Error(t, err)

	m := result.(map[string]any)
	assert.Equal(t, "", m["stdout"])
	assert.Equal(t, "error\n", m["stderr"])
	assert.Equal(t, 1, m["exit_code"])
}

func TestBash_TimeoutFloat(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep command not available")
	}

	start := time.Now()
	_, err := Bash(context.Background(), map[string]any{
		"command":         "sleep 10",
		"timeout_seconds": 1.0,
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Less(t, elapsed, 3*time.Second)
}

func TestBash_EchoWithTimeoutParam(t *testing.T) {
	t.Parallel()
	result, err := Bash(context.Background(), map[string]any{
		"command":         "echo hello",
		"timeout_seconds": 5,
	})
	require.NoError(t, err)

	m := result.(map[string]any)
	assert.Equal(t, "hello\n", m["stdout"])
}