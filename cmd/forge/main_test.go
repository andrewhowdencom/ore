package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWithPath(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantErr string
	}{
		{
			name: "missing file",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "nonexistent.yaml")
			},
			wantErr: "open manifest",
		},
		{
			name: "malformed YAML",
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "bad.yaml")
				require.NoError(t, os.WriteFile(path, []byte("not: valid: yaml: ["), 0644))
				return path
			},
			wantErr: "parse manifest",
		},
		{
			name: "missing required fields",
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "incomplete.yaml")
				require.NoError(t, os.WriteFile(path, []byte("dist:\n  name: agent\n"), 0644))
				return path
			},
			wantErr: "dist.output_path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)
			err := runWithPath(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestBuildCommand_ErrorPaths(t *testing.T) {
	tests := []struct {
		name      string
		setupArgs func(t *testing.T) []string
		wantErr   string
	}{
		{
			name: "missing file",
			setupArgs: func(t *testing.T) []string {
				return []string{"build", "--config", filepath.Join(t.TempDir(), "nonexistent.yaml")}
			},
			wantErr: "open manifest",
		},
		{
			name: "malformed YAML",
			setupArgs: func(t *testing.T) []string {
				path := filepath.Join(t.TempDir(), "bad.yaml")
				require.NoError(t, os.WriteFile(path, []byte("not: valid: yaml: ["), 0644))
				return []string{"build", "--config", path}
			},
			wantErr: "parse manifest",
		},
		{
			name: "missing required fields",
			setupArgs: func(t *testing.T) []string {
				path := filepath.Join(t.TempDir(), "incomplete.yaml")
				require.NoError(t, os.WriteFile(path, []byte("dist:\n  name: agent\n"), 0644))
				return []string{"build", "--config", path}
			},
			wantErr: "dist.output_path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.setupArgs(t)
			cmd := newForgeCmd()
			cmd.SetArgs(args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestGenerateCommand_ErrorPaths(t *testing.T) {
	tests := []struct {
		name      string
		setupArgs func(t *testing.T) []string
		wantErr   string
	}{
		{
			name: "missing file",
			setupArgs: func(t *testing.T) []string {
				return []string{"generate", "--config", filepath.Join(t.TempDir(), "nonexistent.yaml")}
			},
			wantErr: "open manifest",
		},
		{
			name: "malformed YAML",
			setupArgs: func(t *testing.T) []string {
				path := filepath.Join(t.TempDir(), "bad.yaml")
				require.NoError(t, os.WriteFile(path, []byte("not: valid: yaml: ["), 0644))
				return []string{"generate", "--config", path}
			},
			wantErr: "parse manifest",
		},
		{
			name: "missing required fields",
			setupArgs: func(t *testing.T) []string {
				path := filepath.Join(t.TempDir(), "incomplete.yaml")
				require.NoError(t, os.WriteFile(path, []byte("dist:\n  name: agent\n"), 0644))
				return []string{"generate", "--config", path}
			},
			wantErr: "dist.output_path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.setupArgs(t)
			cmd := newForgeCmd()
			cmd.SetArgs(args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestVersionCommand(t *testing.T) {
	var buf bytes.Buffer
	cmd := newForgeCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"version"})
	require.NoError(t, cmd.Execute())
	assert.NotEmpty(t, buf.String())
}

func TestNormalizeArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "converts single-dash config",
			args:     []string{"-config", "forge.yaml"},
			expected: []string{"--config", "forge.yaml"},
		},
		{
			name:     "converts single-dash config with equals",
			args:     []string{"-config=forge.yaml"},
			expected: []string{"--config=forge.yaml"},
		},
		{
			name:     "leaves double-dash alone",
			args:     []string{"--config", "forge.yaml"},
			expected: []string{"--config", "forge.yaml"},
		},
		{
			name:     "leaves unknown single-dash alone",
			args:     []string{"-x"},
			expected: []string{"-x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeArgs(tt.args)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestRootCommand_DefaultsToBuild(t *testing.T) {
	cmd := newForgeCmd()
	cmd.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "nonexistent.yaml")})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open manifest")
}

func TestLogLevel_Invalid(t *testing.T) {
	cmd := newForgeCmd()
	cmd.SetArgs([]string{"--log-level", "invalid", "version"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log level")
}

func TestLogLevel_Valid(t *testing.T) {
	var buf bytes.Buffer
	cmd := newForgeCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--log-level", "debug", "version"})
	require.NoError(t, cmd.Execute())
	assert.NotEmpty(t, buf.String())
}
