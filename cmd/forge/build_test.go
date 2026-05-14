package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuild(t *testing.T) {
	oreModulePath, err := FindOreModuleRoot(".")
	require.NoError(t, err)

	tests := []struct {
		name     string
		manifest *Manifest
	}{
		{
			name: "http",
			manifest: &Manifest{
				Dist:    Dist{Name: "http-agent", OutputPath: "http-agent"},
				Conduit: Conduit{Type: "http"},
			},
		},
		{
			name: "tui",
			manifest: &Manifest{
				Dist:    Dist{Name: "tui-agent", OutputPath: "tui-agent"},
				Conduit: Conduit{Type: "tui"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := t.TempDir()
			outputPath := filepath.Join(outputDir, tt.manifest.Dist.OutputPath)

			err := Build(tt.manifest, oreModulePath, outputPath)
			require.NoError(t, err)

			info, err := os.Stat(outputPath)
			require.NoError(t, err)
			assert.False(t, info.IsDir())
		})
	}
}

func TestBuild_RelativeOutputPath(t *testing.T) {
	oreModulePath, err := FindOreModuleRoot(".")
	require.NoError(t, err)

	t.Chdir(t.TempDir())

	manifest := &Manifest{
		Dist:    Dist{Name: "rel-agent", OutputPath: "rel-agent"},
		Conduit: Conduit{Type: "http"},
	}

	err = Build(manifest, oreModulePath, manifest.Dist.OutputPath)
	require.NoError(t, err)

	info, err := os.Stat(manifest.Dist.OutputPath)
	require.NoError(t, err)
	assert.False(t, info.IsDir())
}
