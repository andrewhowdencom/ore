package main

import (
	"os"
	"path/filepath"
	"testing"

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
