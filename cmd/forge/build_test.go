package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestBuildExternalModule(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()

	var calls [][]string
	execCommand = func(name string, arg ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, arg...))
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/c", "exit", "0")
		}
		return exec.Command("true")
	}

	blueprint := &Blueprint{
		Dist:     Dist{Name: "ext-agent", OutputPath: "ext-agent"},
		Conduits: []ConduitConfig{{Module: "example.com/my/conduit"}},
	}

	oreModulePath, err := FindOreModuleRoot(".")
	require.NoError(t, err)

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "ext-agent")

	err = Build(blueprint, oreModulePath, outputPath)
	require.NoError(t, err)

	require.Len(t, calls, 3)
	assert.Equal(t, []string{"go", "get", "example.com/my/conduit"}, calls[0])
	assert.Equal(t, []string{"go", "mod", "tidy"}, calls[1])
	assert.Equal(t, []string{"go", "build", "-o", outputPath, "."}, calls[2])
}

func TestBuildExternalModuleFailure(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/c", "exit", "1")
		}
		return exec.Command("false")
	}

	blueprint := &Blueprint{
		Dist:     Dist{Name: "ext-agent", OutputPath: "ext-agent"},
		Conduits: []ConduitConfig{{Module: "example.com/my/conduit"}},
	}

	oreModulePath, err := FindOreModuleRoot(".")
	require.NoError(t, err)

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "ext-agent")

	err = Build(blueprint, oreModulePath, outputPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go get example.com/my/conduit")
}
