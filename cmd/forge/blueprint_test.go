package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBlueprint(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Blueprint
		wantErr string
	}{
		{
			name: "valid http blueprint",
			input: `
dist:
  name: my-http-agent
  output_path: ./my-http-agent
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
`,
			want: &Blueprint{
				Dist: Dist{Name: "my-http-agent", OutputPath: "./my-http-agent"},
				Conduits: []ConduitConfig{
					{Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
				},
			},
		},
		{
			name: "valid tui blueprint",
			input: `
dist:
  name: my-tui-agent
  output_path: ./my-tui-agent
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/tui
`,
			want: &Blueprint{
				Dist: Dist{Name: "my-tui-agent", OutputPath: "./my-tui-agent"},
				Conduits: []ConduitConfig{
					{Module: "github.com/andrewhowdencom/ore/x/conduit/tui"},
				},
			},
		},
		{
			name: "missing dist.name",
			input: `
dist:
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
`,
			wantErr: "dist.name is required",
		},
		{
			name: "missing dist.output_path",
			input: `
dist:
  name: agent
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
`,
			wantErr: "dist.output_path is required",
		},
		{
			name: "empty conduits",
			input: `
dist:
  name: agent
  output_path: ./out
conduits: []
`,
			wantErr: "conduits must contain at least one entry",
		},
		{
			name: "missing conduit module",
			input: `
dist:
  name: agent
  output_path: ./out
conduits:
  - module: ""
`,
			wantErr: "conduits[0].module is required",
		},
		{
			name: "duplicate conduit entries",
			input: `
dist:
  name: dup-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
  - module: github.com/andrewhowdencom/ore/x/conduit/http
`,
			want: &Blueprint{
				Dist: Dist{Name: "dup-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
					{Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
				},
			},
		},
		{
			name: "malformed YAML",
			input:   "not: valid: yaml: [",
			wantErr: "decode blueprint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBlueprint(strings.NewReader(tt.input))
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
