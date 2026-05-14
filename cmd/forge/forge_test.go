package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForgeSmoke(t *testing.T) {
	oreModulePath, err := FindOreModuleRoot(".")
	require.NoError(t, err)

	tests := []struct {
		name         string
		manifestPath string
	}{
		{
			name:         "http",
			manifestPath: "testdata/http-forge.yaml",
		},
		{
			name:         "tui",
			manifestPath: "testdata/tui-forge.yaml",
		},
		{
			name:         "http-example",
			manifestPath: "../../examples/forge/http/forge.yaml",
		},
		{
			name:         "tui-example",
			manifestPath: "../../examples/forge/tui/forge.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.Open(tt.manifestPath)
			require.NoError(t, err)
			defer f.Close()

			manifest, err := ParseManifest(f)
			require.NoError(t, err)

			outputDir := t.TempDir()
			outputPath := filepath.Join(outputDir, filepath.Base(manifest.Dist.OutputPath))

			err = Build(manifest, oreModulePath, outputPath)
			require.NoError(t, err)

			info, err := os.Stat(outputPath)
			require.NoError(t, err)
			assert.False(t, info.IsDir())
		})
	}
}

func TestForgeSmoke_RuntimeGuard(t *testing.T) {
	oreModulePath, err := FindOreModuleRoot(".")
	require.NoError(t, err)

	manifest := &Manifest{
		Dist:    Dist{Name: "guard-agent", OutputPath: "guard-agent"},
		Conduit: Conduit{Type: "http"},
	}

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, manifest.Dist.OutputPath)

	err = Build(manifest, oreModulePath, outputPath)
	require.NoError(t, err)

	cmd := exec.Command(outputPath)
	cmd.Env = []string{} // clear environment so ORE_API_KEY is unset
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Start())

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		require.Error(t, err)
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.NotZero(t, exitErr.ExitCode())
		assert.Contains(t, stderr.String(), "ORE_API_KEY not set")
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("binary did not exit within timeout")
	}
}
