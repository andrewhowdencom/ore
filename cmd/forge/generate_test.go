package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMainGo(t *testing.T) {
	tests := []struct {
		name     string
		manifest *Manifest
	}{
		{
			name: "http conduit",
			manifest: &Manifest{
				Dist:    Dist{Name: "http-agent", OutputPath: "./out"},
				Conduit: Conduit{Type: "http"},
			},
		},
		{
			name: "tui conduit",
			manifest: &Manifest{
				Dist:    Dist{Name: "tui-agent", OutputPath: "./out"},
				Conduit: Conduit{Type: "tui"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateMainGo(tt.manifest)
			require.NoError(t, err)

			content := string(got)
			if tt.manifest.Conduit.Type == "http" {
				assert.Contains(t, content, `"net/http"`)
				assert.Contains(t, content, `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`)
				assert.NotContains(t, content, `"flag"`)
			} else {
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/x/conduit/tui"`)
				assert.Contains(t, content, `"flag"`)
			}
		})
	}
}

func TestGenerateGoMod(t *testing.T) {
	manifest := &Manifest{
		Dist:    Dist{Name: "test-agent", OutputPath: "./out"},
		Conduit: Conduit{Type: "http"},
	}

	got, err := GenerateGoMod(manifest, "/absolute/path/to/ore")
	require.NoError(t, err)

	content := string(got)
	assert.Contains(t, content, "module test-agent")
	assert.Contains(t, content, "go 1.26.2")
	assert.Contains(t, content, "require github.com/andrewhowdencom/ore v0.0.0")
	assert.Contains(t, content, "replace github.com/andrewhowdencom/ore => /absolute/path/to/ore")
}
