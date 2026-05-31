package bash

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testFileSandbox struct {
	dir string
}

func (m *testFileSandbox) Name() string { return "test" }
func (m *testFileSandbox) ResolvePath(path string) (string, error) { return path, nil }
func (m *testFileSandbox) WorkingDirectory() string { return m.dir }

var _ tool.FileSandbox = (*testFileSandbox)(nil)

func newTestSandbox(t *testing.T) *testFileSandbox {
	return &testFileSandbox{dir: t.TempDir()}
}

type mockExecSandbox struct {
	dir     string
	runFunc func(ctx context.Context, cmd, dir string, timeout time.Duration) (string, string, int, error)
}

func (m *mockExecSandbox) Name() string { return "mock-exec" }
func (m *mockExecSandbox) ResolvePath(path string) (string, error) { return path, nil }
func (m *mockExecSandbox) WorkingDirectory() string { return m.dir }
func (m *mockExecSandbox) Run(ctx context.Context, cmd, dir string, timeout time.Duration) (string, string, int, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, cmd, dir, timeout)
	}
	return "mock-out", "mock-err", 0, nil
}

var _ tool.ExecSandbox = (*mockExecSandbox)(nil)

func TestBash_NilSandbox(t *testing.T) {
	t.Parallel()
	_, err := Bash(context.Background(), nil, map[string]any{"command": "echo hello"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox required")
}

func TestBash_Echo(t *testing.T) {
	t.Parallel()
	sb := newTestSandbox(t)
	result, err := Bash(context.Background(), sb, map[string]any{"command": "echo hello"})
	require.NoError(t, err)

	r := result.(*Result)
	assert.Equal(t, "hello\n", r.Stdout)
	assert.Equal(t, "", r.Stderr)
	assert.Equal(t, 0, r.ExitCode)
}

func TestBash_InvalidCommand(t *testing.T) {
	t.Parallel()
	sb := newTestSandbox(t)
	result, err := Bash(context.Background(), sb, map[string]any{"command": "exit 42"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited with code 42")

	r := result.(*Result)
	assert.Equal(t, "", r.Stdout)
	assert.Equal(t, "", r.Stderr)
	assert.Equal(t, 42, r.ExitCode)
}

func TestBash_WorkingDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sb := newTestSandbox(t)

	result, err := Bash(context.Background(), sb, map[string]any{
		"command":           "pwd",
		"working_directory": dir,
	})
	require.NoError(t, err)

	r := result.(*Result)
	assert.Contains(t, r.Stdout, filepath.Base(dir))
}

func TestBash_Timeout(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep command not available")
	}

	sb := newTestSandbox(t)
	start := time.Now()
	_, err := Bash(context.Background(), sb, map[string]any{
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
	sb := newTestSandbox(t)
	_, err := Bash(context.Background(), sb, map[string]any{"command": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestBash_StderrCapture(t *testing.T) {
	t.Parallel()
	sb := newTestSandbox(t)
	result, err := Bash(context.Background(), sb, map[string]any{"command": "echo error >&2; exit 1"})
	require.Error(t, err)

	r := result.(*Result)
	assert.Equal(t, "", r.Stdout)
	assert.Equal(t, "error\n", r.Stderr)
	assert.Equal(t, 1, r.ExitCode)
}

func TestBash_TimeoutFloat(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep command not available")
	}

	sb := newTestSandbox(t)
	start := time.Now()
	_, err := Bash(context.Background(), sb, map[string]any{
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
	sb := newTestSandbox(t)
	result, err := Bash(context.Background(), sb, map[string]any{
		"command":         "echo hello",
		"timeout_seconds": 5,
	})
	require.NoError(t, err)

	r := result.(*Result)
	assert.Equal(t, "hello\n", r.Stdout)
}

func TestBash_ExecSandboxDelegation(t *testing.T) {
	t.Parallel()
	var calledCmd, calledDir string
	var calledTimeout time.Duration
	sb := &mockExecSandbox{
		dir: "/mock/dir",
		runFunc: func(ctx context.Context, cmd, dir string, timeout time.Duration) (string, string, int, error) {
			calledCmd = cmd
			calledDir = dir
			calledTimeout = timeout
			return "out", "err", 0, nil
		},
	}

	result, err := Bash(context.Background(), sb, map[string]any{
		"command": "echo hello",
	})
	require.NoError(t, err)

	assert.Equal(t, "echo hello", calledCmd)
	assert.Equal(t, "/mock/dir", calledDir)
	assert.Equal(t, 30*time.Second, calledTimeout)

	r := result.(*Result)
	assert.Equal(t, "out", r.Stdout)
	assert.Equal(t, "err", r.Stderr)
	assert.Equal(t, 0, r.ExitCode)
}

func TestBash_ExecSandboxWithWorkingDir(t *testing.T) {
	t.Parallel()
	var calledDir string
	sb := &mockExecSandbox{
		dir: "/mock/default",
		runFunc: func(ctx context.Context, cmd, dir string, timeout time.Duration) (string, string, int, error) {
			calledDir = dir
			return "", "", 0, nil
		},
	}

	_, err := Bash(context.Background(), sb, map[string]any{
		"command":           "pwd",
		"working_directory": "/custom/dir",
	})
	require.NoError(t, err)

	assert.Equal(t, "/custom/dir", calledDir)
}

func TestBash_ExecSandboxError(t *testing.T) {
	t.Parallel()
	sb := &mockExecSandbox{
		runFunc: func(ctx context.Context, cmd, dir string, timeout time.Duration) (string, string, int, error) {
			return "partial output", "error details", 1, fmt.Errorf("mock error")
		},
	}

	result, err := Bash(context.Background(), sb, map[string]any{
		"command": "fail",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited with code 1")

	r := result.(*Result)
	assert.Equal(t, "partial output", r.Stdout)
	assert.Equal(t, "error details", r.Stderr)
	assert.Equal(t, 1, r.ExitCode)
}

func TestBash_FileSandboxFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sb := &testFileSandbox{dir: dir}

	result, err := Bash(context.Background(), sb, map[string]any{
		"command": "pwd",
	})
	require.NoError(t, err)

	r := result.(*Result)
	assert.Contains(t, r.Stdout, filepath.Base(dir))
}

func TestBash_NonNumericTimeout(t *testing.T) {
	t.Parallel()
	var calledTimeout time.Duration
	sb := &mockExecSandbox{
		runFunc: func(ctx context.Context, cmd, dir string, timeout time.Duration) (string, string, int, error) {
			calledTimeout = timeout
			return "", "", 0, nil
		},
	}

	_, err := Bash(context.Background(), sb, map[string]any{
		"command":         "echo hello",
		"timeout_seconds": "abc",
	})
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, calledTimeout)
}
