package main

import (
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
