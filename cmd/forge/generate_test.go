package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMainGo(t *testing.T) {
	tests := []struct {
		name      string
		blueprint *Blueprint
		check     func(t *testing.T, content string)
	}{
		{
			name: "http conduit",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "http-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`)
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/agent"`)
				assert.Contains(t, content, `a := agent.New(mgr)`)
				assert.Contains(t, content, `c0, err := httpc.New(mgr, httpc.WithUI(), httpc.WithAddr(":" + port))`)
				assert.Contains(t, content, `a.Add(c0)`)
				assert.Contains(t, content, `return a.Run(ctx)`)
				assert.Contains(t, content, `port := os.Getenv("PORT")`)
				assert.NotContains(t, content, `"flag"`)
				assert.NotContains(t, content, `"github.com/andrewhowdencom/ore/x/conduit/tui"`)
			},
		},
		{
			name: "tui conduit",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "tui-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/tui"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/x/conduit/tui"`)
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/agent"`)
				assert.Contains(t, content, `a := agent.New(mgr)`)
				assert.Contains(t, content, `c0, err := tui.New(mgr, tui.WithThreadID(threadID))`)
				assert.Contains(t, content, `a.Add(c0)`)
				assert.Contains(t, content, `return a.Run(ctx)`)
				assert.Contains(t, content, `"flag"`)
				assert.Contains(t, content, `flag.StringVar(&threadID, "thread", "", "existing thread UUID to resume")`)
				assert.NotContains(t, content, `port := os.Getenv("PORT")`)
			},
		},
		{
			name: "multi-conduit http+tui",
			blueprint: &Blueprint{
				Dist: Dist{Name: "multi-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
					{Module: "github.com/andrewhowdencom/ore/x/conduit/tui"},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`)
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/x/conduit/tui"`)
				assert.Contains(t, content, `"flag"`)
				assert.Contains(t, content, `port := os.Getenv("PORT")`)
				assert.Contains(t, content, `c0, err := httpc.New(mgr, httpc.WithUI(), httpc.WithAddr(":" + port))`)
				assert.Contains(t, content, `c1, err := tui.New(mgr, tui.WithThreadID(threadID))`)
				assert.Contains(t, content, `a.Add(c0)`)
				assert.Contains(t, content, `a.Add(c1)`)
				assert.Contains(t, content, `return a.Run(ctx)`)
			},
		},
		{
			name: "external conduit",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "ext-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Module: "example.com/my/conduit"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `conduit "example.com/my/conduit"`)
				assert.Contains(t, content, `c0, err := conduit.New(mgr)`)
				assert.NotContains(t, content, `"flag"`)
				assert.NotContains(t, content, `port := os.Getenv("PORT")`)
			},
		},
		{
			name: "duplicate alias disambiguation",
			blueprint: &Blueprint{
				Dist: Dist{Name: "dup-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{Module: "example.com/my/conduit"},
					{Module: "other.com/my/conduit"},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `conduit "example.com/my/conduit"`)
				assert.Contains(t, content, `conduit1 "other.com/my/conduit"`)
				assert.Contains(t, content, `c0, err := conduit.New(mgr)`)
				assert.Contains(t, content, `c1, err := conduit1.New(mgr)`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateMainGo(tt.blueprint)
			require.NoError(t, err)
			tt.check(t, string(got))
		})
	}
}

func TestGenerateGoMod(t *testing.T) {
	blueprint := &Blueprint{
		Dist:     Dist{Name: "test-agent", OutputPath: "./out"},
		Conduits: []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
	}

	got, err := GenerateGoMod(blueprint, "/absolute/path/to/ore")
	require.NoError(t, err)

	content := string(got)
	assert.Contains(t, content, "module test-agent")
	assert.Contains(t, content, "go 1.26.2")
	assert.Contains(t, content, "require github.com/andrewhowdencom/ore v0.0.0")
	assert.Contains(t, content, "replace github.com/andrewhowdencom/ore => /absolute/path/to/ore")
}
