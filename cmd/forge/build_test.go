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
		name      string
		blueprint *Blueprint
	}{
		{
			name: "http",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "http-agent", OutputPath: "http-agent"},
				Conduits: []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
			},
		},
		{
			name: "tui",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "tui-agent", OutputPath: "tui-agent"},
				Conduits: []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/tui"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := t.TempDir()
			outputPath := filepath.Join(outputDir, tt.blueprint.Dist.OutputPath)

			err := Build(tt.blueprint, oreModulePath, outputPath)
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

	blueprint := &Blueprint{
		Dist:     Dist{Name: "rel-agent", OutputPath: "rel-agent"},
		Conduits: []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
	}

	err = Build(blueprint, oreModulePath, blueprint.Dist.OutputPath)
	require.NoError(t, err)

	info, err := os.Stat(blueprint.Dist.OutputPath)
	require.NoError(t, err)
	assert.False(t, info.IsDir())
}
