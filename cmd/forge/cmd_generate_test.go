package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCommand(t *testing.T) {
	tests := []struct {
		name      string
		setupArgs func(t *testing.T) []string
		checkOut  func(t *testing.T, out string)
		checkDir  func(t *testing.T, dir string)
		wantErr   string
	}{
		{
			name: "stdout output http",
			setupArgs: func(t *testing.T) []string {
				return []string{"generate", "--config", "testdata/http-forge.yaml"}
			},
			checkOut: func(t *testing.T, out string) {
				assert.Contains(t, out, "// --- FILE: go.mod ---")
				assert.Contains(t, out, "module http-smoke-agent")
			},
		},
		{
			name: "stdout output tui",
			setupArgs: func(t *testing.T) []string {
				return []string{"generate", "--config", "testdata/tui-forge.yaml"}
			},
			checkOut: func(t *testing.T, out string) {
				assert.Contains(t, out, "// --- FILE: go.mod ---")
				assert.Contains(t, out, `"github.com/andrewhowdencom/ore/x/conduit/tui"`)
				assert.Contains(t, out, "module tui-smoke-agent")
			},
		},
		{
			name: "directory output http",
			setupArgs: func(t *testing.T) []string {
				return []string{"generate", "--config", "testdata/http-forge.yaml", "-o", t.TempDir()}
			},
			checkOut: func(t *testing.T, out string) {
				assert.Empty(t, out)
			},
			checkDir: func(t *testing.T, dir string) {
				mainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
				require.NoError(t, err)
				assert.Contains(t, string(mainGo), `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`)

				goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
				require.NoError(t, err)
				assert.Contains(t, string(goMod), "module http-smoke-agent")
			},
		},
		{
			name: "directory output tui",
			setupArgs: func(t *testing.T) []string {
				return []string{"generate", "--config", "testdata/tui-forge.yaml", "-o", t.TempDir()}
			},
			checkOut: func(t *testing.T, out string) {
				assert.Empty(t, out)
			},
			checkDir: func(t *testing.T, dir string) {
				mainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
				require.NoError(t, err)
				assert.Contains(t, string(mainGo), `"github.com/andrewhowdencom/ore/x/conduit/tui"`)

				goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
				require.NoError(t, err)
				assert.Contains(t, string(goMod), "module tui-smoke-agent")
			},
		},
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
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetArgs(args)

			err := cmd.Execute()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			if tt.checkOut != nil {
				tt.checkOut(t, buf.String())
			}
			if tt.checkDir != nil {
				for i := 0; i < len(args)-1; i++ {
					if args[i] == "-o" || args[i] == "--output" {
						tt.checkDir(t, args[i+1])
						break
					}
				}
			}
		})
	}
}
