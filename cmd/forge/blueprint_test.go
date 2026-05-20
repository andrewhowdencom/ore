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
					{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
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
					{Name: "tui", Module: "github.com/andrewhowdencom/ore/x/conduit/tui"},
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
					{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
					{Name: "http1", Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
				},
			},
		},
		{
			name: "valid blueprint with handlers",
			input: `
dist:
  name: handler-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
handlers:
  - module: github.com/andrewhowdencom/ore/tool
`,
			want: &Blueprint{
				Dist:     Dist{Name: "handler-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Handlers: []HandlerConfig{{Name: "tool", Module: "github.com/andrewhowdencom/ore/tool"}},
			},
		},
		{
			name: "valid blueprint with handler options",
			input: `
dist:
  name: handler-opts-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
handlers:
  - module: github.com/andrewhowdencom/ore/tool
    options:
      verbose: true
`,
			want: &Blueprint{
				Dist:     Dist{Name: "handler-opts-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Handlers: []HandlerConfig{
					{Name: "tool", Module: "github.com/andrewhowdencom/ore/tool", Options: map[string]any{"verbose": true}},
				},
			},
		},
		{
			name: "empty handlers list",
			input: `
dist:
  name: no-handler-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
handlers: []
`,
			want: &Blueprint{
				Dist:     Dist{Name: "no-handler-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Handlers: []HandlerConfig{},
			},
		},
		{
			name: "missing handler module",
			input: `
dist:
  name: bad-handler-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
handlers:
  - module: ""
`,
			wantErr: "handlers[0].module is required",
		},
		{
			name: "explicit names",
			input: `
dist:
  name: explicit-agent
  output_path: ./out
conduits:
  - name: public-api
    module: github.com/andrewhowdencom/ore/x/conduit/http
  - name: internal-admin
    module: github.com/andrewhowdencom/ore/x/conduit/http
`,
			want: &Blueprint{
				Dist: Dist{Name: "explicit-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{Name: "public-api", Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
					{Name: "internal-admin", Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
				},
			},
		},
		{
			name: "duplicate explicit names",
			input: `
dist:
  name: dup-name-agent
  output_path: ./out
conduits:
  - name: api
    module: github.com/andrewhowdencom/ore/x/conduit/http
  - name: api
    module: github.com/andrewhowdencom/ore/x/conduit/tui
`,
			wantErr: "duplicate conduit/handler name: api",
		},
		{
			name: "conduit and handler name collision",
			input: `
dist:
  name: collision-agent
  output_path: ./out
conduits:
  - name: shared
    module: github.com/andrewhowdencom/ore/x/conduit/http
handlers:
  - name: shared
    module: github.com/andrewhowdencom/ore/tool
`,
			wantErr: "duplicate conduit/handler name: shared",
		},
		{
			name: "explicit name avoids derived collision",
			input: `
dist:
  name: avoid-collision-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
  - name: http
    module: github.com/andrewhowdencom/ore/x/conduit/tui
`,
			want: &Blueprint{
				Dist: Dist{Name: "avoid-collision-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{Name: "http1", Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
					{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/tui"},
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

func TestParseBlueprint_Transforms(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Blueprint
		wantErr string
	}{
		{
			name: "valid blueprint with transforms",
			input: `
dist:
  name: transform-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
transforms:
  - module: github.com/andrewhowdencom/ore/x/systemprompt
`,
			want: &Blueprint{
				Dist:       Dist{Name: "transform-agent", OutputPath: "./out"},
				Conduits:   []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Transforms: []TransformConfig{{Module: "github.com/andrewhowdencom/ore/x/systemprompt"}},
			},
		},
		{
			name: "valid blueprint with transform options",
			input: `
dist:
  name: transform-opts-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
transforms:
  - module: github.com/andrewhowdencom/ore/x/systemprompt
    options:
      content: "You are a helpful assistant."
`,
			want: &Blueprint{
				Dist:     Dist{Name: "transform-opts-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Transforms: []TransformConfig{
					{Module: "github.com/andrewhowdencom/ore/x/systemprompt", Options: map[string]any{"content": "You are a helpful assistant."}},
				},
			},
		},
		{
			name: "empty transforms list",
			input: `
dist:
  name: no-transform-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
transforms: []
`,
			want: &Blueprint{
				Dist:       Dist{Name: "no-transform-agent", OutputPath: "./out"},
				Conduits:   []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Transforms: []TransformConfig{},
			},
		},
		{
			name: "missing transform module",
			input: `
dist:
  name: bad-transform-agent
  output_path: ./out
conduits:
  - module: github.com/andrewhowdencom/ore/x/conduit/http
transforms:
  - module: ""
`,
			wantErr: "transforms[0].module is required",
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
