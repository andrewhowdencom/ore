package unsafe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/tool/bash"
	"github.com/andrewhowdencom/ore/x/tool/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface check.
var _ tool.Sandbox = (*Sandbox)(nil)

func TestNew_Name(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		wantName string
	}{
		{"foo", "foo"},
		{"bar", "bar"},
		{"", ""},
		{"test-sandbox", "test-sandbox"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sb := New(tt.name)
			assert.Equal(t, tt.wantName, sb.Name())
		})
	}
}

func TestSandbox_Interface(t *testing.T) {
	t.Parallel()

	// Verify the returned type is the concrete type.
	sb := New("interface-check")
	assert.Implements(t, (*tool.Sandbox)(nil), sb)

	// Verify we can type-assert to the concrete type.
	_, ok := sb.(*Sandbox)
	assert.True(t, ok, "expected sb to be *Sandbox")
}

func TestSandbox_NegativeInterfaces(t *testing.T) {
	t.Parallel()

	sb := New("negative-check")

	// Verify Sandbox does NOT implement FileSandbox.
	_, ok := sb.(tool.FileSandbox)
	assert.False(t, ok, "expected Sandbox to NOT implement tool.FileSandbox")

	// Verify Sandbox does NOT implement ExecSandbox.
	_, ok = sb.(tool.ExecSandbox)
	assert.False(t, ok, "expected Sandbox to NOT implement tool.ExecSandbox")
}

func TestSandbox_ConcurrentNew(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-%d", i)
			sb := New(name)
			assert.Equal(t, name, sb.Name())
		}(i)
	}
	wg.Wait()
}

func TestSandbox_BashIntegration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sb := New("bash-integration")

	result, err := bash.Bash(ctx, sb, map[string]any{"command": "echo hello"})
	require.NoError(t, err, "bash.Bash should not fail with unsafe sandbox")

	r := result.(*bash.Result)
	assert.Equal(t, "hello\n", r.Stdout)
	assert.Equal(t, "", r.Stderr)
	assert.Equal(t, 0, r.ExitCode)
}

func TestSandbox_BashIntegration_WorkingDirectory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sb := New("bash-integration-wd")
	dir := t.TempDir()

	result, err := bash.Bash(ctx, sb, map[string]any{
		"command":           "pwd",
		"working_directory": dir,
	})
	require.NoError(t, err)

	r := result.(*bash.Result)
	assert.Contains(t, r.Stdout, filepath.Base(dir))
}

func TestSandbox_FilesystemIntegration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sb := New("fs-integration")

	// Create a temp file with absolute path.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("hello world"), 0o644)
	require.NoError(t, err)

	// ReadFile with absolute path and unsafe sandbox should succeed
	// (paths pass through since unsafe.Sandbox does not implement FileSandbox).
	result, err := filesystem.ReadFile(ctx, sb, map[string]any{"path": tmpFile})
	require.NoError(t, err)

	content := result.(string)
	assert.Contains(t, content, "1|hello world")
}
