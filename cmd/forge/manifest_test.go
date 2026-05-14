package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseManifest(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Manifest
		wantErr string
	}{
		{
			name: "valid http manifest",
			input: `
dist:
  name: my-http-agent
  output_path: ./my-http-agent
conduit:
  type: http
`,
			want: &Manifest{
				Dist:    Dist{Name: "my-http-agent", OutputPath: "./my-http-agent"},
				Conduit: Conduit{Type: "http"},
			},
		},
		{
			name: "valid tui manifest",
			input: `
dist:
  name: my-tui-agent
  output_path: ./my-tui-agent
conduit:
  type: tui
`,
			want: &Manifest{
				Dist:    Dist{Name: "my-tui-agent", OutputPath: "./my-tui-agent"},
				Conduit: Conduit{Type: "tui"},
			},
		},
		{
			name: "missing dist.name",
			input: `
dist:
  output_path: ./out
conduit:
  type: http
`,
			wantErr: "dist.name is required",
		},
		{
			name: "missing dist.output_path",
			input: `
dist:
  name: agent
conduit:
  type: http
`,
			wantErr: "dist.output_path is required",
		},
		{
			name: "unknown conduit type",
			input: `
dist:
  name: agent
  output_path: ./out
conduit:
  type: grpc
`,
			wantErr: `conduit.type must be "http" or "tui"`,
		},
		{
			name: "empty conduit type",
			input: `
dist:
  name: agent
  output_path: ./out
conduit:
  type: ""
`,
			wantErr: `conduit.type must be "http" or "tui"`,
		},
		{
			name: "malformed YAML",
			input:   "not: valid: yaml: [",
			wantErr: "decode manifest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseManifest(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
