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
				Conduits: []ConduitConfig{{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`)
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/app"`)
				assert.Contains(t, content, `app.Run(`)
				assert.Contains(t, content, `app.WithConduit("http"`)
				assert.Contains(t, content, `return httpc.New(mgr)`)
				assert.NotContains(t, content, `"flag"`)
				assert.NotContains(t, content, `"github.com/andrewhowdencom/ore/x/conduit/tui"`)
			},
		},
		{
			name: "tui conduit",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "tui-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Name: "tui", Module: "github.com/andrewhowdencom/ore/x/conduit/tui"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/x/conduit/tui"`)
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/app"`)
				assert.Contains(t, content, `app.Run(`)
				assert.Contains(t, content, `app.WithConduit("tui"`)
				assert.Contains(t, content, `return tui.New(mgr)`)
				assert.NotContains(t, content, `"flag"`)
			},
		},
		{
			name: "multi-conduit http+tui",
			blueprint: &Blueprint{
				Dist: Dist{Name: "multi-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"},
					{Name: "tui", Module: "github.com/andrewhowdencom/ore/x/conduit/tui"},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`)
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/x/conduit/tui"`)
				assert.Contains(t, content, `app.WithConduit("http"`)
				assert.Contains(t, content, `app.WithConduit("tui"`)
				assert.Contains(t, content, `return httpc.New(mgr)`)
				assert.Contains(t, content, `return tui.New(mgr)`)
			},
		},
		{
			name: "external conduit",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "ext-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Name: "conduit", Module: "example.com/my/conduit"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `conduit "example.com/my/conduit"`)
				assert.Contains(t, content, `app.WithConduit("conduit"`)
				assert.Contains(t, content, `return conduit.New(mgr)`)
				assert.NotContains(t, content, `"flag"`)
			},
		},
		{
			name: "duplicate alias disambiguation",
			blueprint: &Blueprint{
				Dist: Dist{Name: "dup-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{Name: "conduit", Module: "example.com/my/conduit"},
					{Name: "conduit1", Module: "other.com/my/conduit"},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `conduit "example.com/my/conduit"`)
				assert.Contains(t, content, `conduit1 "other.com/my/conduit"`)
				assert.Contains(t, content, `app.WithConduit("conduit"`)
				assert.Contains(t, content, `app.WithConduit("conduit1"`)
				assert.Contains(t, content, `return conduit.New(mgr)`)
				assert.Contains(t, content, `return conduit1.New(mgr)`)
			},
		},
		{
			name: "http conduit with options",
			blueprint: &Blueprint{
				Dist: Dist{Name: "http-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{
						Name:    "http",
						Module:  "github.com/andrewhowdencom/ore/x/conduit/http",
						Options: map[string]any{"addr": ":8080", "ui": false},
					},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`)
				assert.Contains(t, content, `map[string]any{"addr": ":8080", "ui": false}`)
				assert.Contains(t, content, `httpcOpts, err := httpc.OptionsFromMap(opts)`)
				assert.Contains(t, content, `return httpc.New(mgr, httpcOpts...)`)
				assert.Contains(t, content, `app.WithConduit("http"`)
			},
		},
		{
			name: "multi-conduit mixed options",
			blueprint: &Blueprint{
				Dist: Dist{Name: "mixed-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{
						Name:    "http",
						Module:  "github.com/andrewhowdencom/ore/x/conduit/http",
						Options: map[string]any{"addr": ":8080"},
					},
					{Name: "tui", Module: "github.com/andrewhowdencom/ore/x/conduit/tui"},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `map[string]any{"addr": ":8080"}`)
				assert.Contains(t, content, `httpcOpts, err := httpc.OptionsFromMap(opts)`)
				assert.Contains(t, content, `return httpc.New(mgr, httpcOpts...)`)
				assert.Contains(t, content, `return tui.New(mgr)`)
				assert.Contains(t, content, `app.WithConduit("http"`)
				assert.Contains(t, content, `app.WithConduit("tui"`)
			},
		},
		{
			name: "triple alias disambiguation",
			blueprint: &Blueprint{
				Dist: Dist{Name: "triple-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{Name: "conduit", Module: "example.com/my/conduit"},
					{Name: "conduit1", Module: "other.com/my/conduit"},
					{Name: "conduit2", Module: "third.com/my/conduit"},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `conduit "example.com/my/conduit"`)
				assert.Contains(t, content, `conduit1 "other.com/my/conduit"`)
				assert.Contains(t, content, `conduit2 "third.com/my/conduit"`)
				assert.Contains(t, content, `app.WithConduit("conduit"`)
				assert.Contains(t, content, `app.WithConduit("conduit1"`)
				assert.Contains(t, content, `app.WithConduit("conduit2"`)
				assert.Contains(t, content, `return conduit.New(mgr)`)
				assert.Contains(t, content, `return conduit1.New(mgr)`)
				assert.Contains(t, content, `return conduit2.New(mgr)`)
			},
		},
		{
			name: "single handler",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "handler-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Handlers: []HandlerConfig{{Name: "tool", Module: "github.com/andrewhowdencom/ore/tool"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `tool "github.com/andrewhowdencom/ore/tool"`)
				assert.Contains(t, content, `"github.com/andrewhowdencom/ore/loop"`)
				assert.Contains(t, content, `app.WithHandler("tool"`)
				assert.Contains(t, content, `return tool.New()`)
				assert.Contains(t, content, `app.WithConduit("http"`)
			},
		},
		{
			name: "handler with options",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "handler-opts-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Handlers: []HandlerConfig{
					{Name: "tool", Module: "github.com/andrewhowdencom/ore/tool", Options: map[string]any{"verbose": true}},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `map[string]any{"verbose": true}`)
				assert.Contains(t, content, `toolOpts, err := tool.OptionsFromMap(opts)`)
				assert.Contains(t, content, `return tool.New(toolOpts...)`)
				assert.Contains(t, content, `app.WithHandler("tool"`)
			},
		},
		{
			name: "conduit and handler alias collision",
			blueprint: &Blueprint{
				Dist: Dist{Name: "collision-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{
					{Name: "handler", Module: "example.com/my/handler"},
				},
				Handlers: []HandlerConfig{
					{Name: "handler1", Module: "other.com/my/handler"},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `handler "example.com/my/handler"`)
				assert.Contains(t, content, `handler1 "other.com/my/handler"`)
				assert.Contains(t, content, `app.WithConduit("handler"`)
				assert.Contains(t, content, `app.WithHandler("handler1"`)
				assert.Contains(t, content, `return handler.New(mgr)`)
				assert.Contains(t, content, `return handler1.New()`)
			},
		},
		{
			name: "http stdlib collision with handler",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "http-collision-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Handlers: []HandlerConfig{{Name: "httpc", Module: "example.com/my/http"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `httpc "github.com/andrewhowdencom/ore/x/conduit/http"`)
				assert.Contains(t, content, `httpc1 "example.com/my/http"`)
				assert.Contains(t, content, `return httpc1.New()`)
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
		Conduits: []ConduitConfig{{Name: "http", Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
	}

	got, err := GenerateGoMod(blueprint, "/absolute/path/to/ore")
	require.NoError(t, err)

	content := string(got)
	assert.Contains(t, content, "module test-agent")
	assert.Contains(t, content, "go 1.26.2")
	assert.Contains(t, content, "require github.com/andrewhowdencom/ore v0.0.0")
	assert.Contains(t, content, "replace github.com/andrewhowdencom/ore => /absolute/path/to/ore")
}

func TestFormatGoMapStringAny(t *testing.T) {
	m := map[string]any{
		"addr":   ":8080",
		"nested": map[string]any{"key": "value"},
		"list":   []any{1, "two", true},
	}
	got := formatGoMapStringAny(m)
	want := `map[string]any{"addr": ":8080", "list": []any{1, "two", true}, "nested": map[string]any{"key": "value"}}`
	assert.Equal(t, want, got)
}

func TestGenerateMainGo_Transforms(t *testing.T) {
	tests := []struct {
		name      string
		blueprint *Blueprint
		check     func(t *testing.T, content string)
	}{
		{
			name: "single transform",
			blueprint: &Blueprint{
				Dist:       Dist{Name: "transform-agent", OutputPath: "./out"},
				Conduits:   []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Transforms: []TransformConfig{{Module: "github.com/andrewhowdencom/ore/x/systemprompt"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `systemprompt "github.com/andrewhowdencom/ore/x/systemprompt"`)
				assert.Contains(t, content, `app.WithTransform("systemprompt",`)
				assert.Contains(t, content, `tr, err := systemprompt.New()`)
				assert.Contains(t, content, `return tr.AsTransform(), nil`)
			},
		},
		{
			name: "transform with options",
			blueprint: &Blueprint{
				Dist:     Dist{Name: "transform-opts-agent", OutputPath: "./out"},
				Conduits: []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Transforms: []TransformConfig{
					{Module: "github.com/andrewhowdencom/ore/x/systemprompt", Options: map[string]any{"content": "You are a helpful assistant."}},
				},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `systempromptOpts, err := systemprompt.OptionsFromMap(opts)`)
				assert.Contains(t, content, `tr, err := systemprompt.New(systempromptOpts...)`)
				assert.Contains(t, content, `return tr.AsTransform(), nil`)
				assert.Contains(t, content, `}, map[string]any{"content": "You are a helpful assistant."}),`)
			},
		},
		{
			name: "transform and handler",
			blueprint: &Blueprint{
				Dist:       Dist{Name: "both-agent", OutputPath: "./out"},
				Conduits:   []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Handlers:   []HandlerConfig{{Module: "github.com/andrewhowdencom/ore/tool"}},
				Transforms: []TransformConfig{{Module: "github.com/andrewhowdencom/ore/x/systemprompt"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `systemprompt "github.com/andrewhowdencom/ore/x/systemprompt"`)
				assert.Contains(t, content, `tool "github.com/andrewhowdencom/ore/tool"`)
				assert.Contains(t, content, `app.WithTransform("systemprompt",`)
				assert.Contains(t, content, `app.WithHandler("",`) // test does not set handler name
				assert.Contains(t, content, `tr, err := systemprompt.New()`)
				assert.Contains(t, content, `return tr.AsTransform(), nil`)
				assert.Contains(t, content, `return tool.New()`)
			},
		},
		{
			name: "transform alias collision with handler",
			blueprint: &Blueprint{
				Dist:       Dist{Name: "collision-agent", OutputPath: "./out"},
				Conduits:   []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
				Handlers:   []HandlerConfig{{Module: "example.com/my/systemprompt"}},
				Transforms: []TransformConfig{{Module: "github.com/andrewhowdencom/ore/x/systemprompt"}},
			},
			check: func(t *testing.T, content string) {
				assert.Contains(t, content, `systemprompt "example.com/my/systemprompt"`)
				assert.Contains(t, content, `systemprompt1 "github.com/andrewhowdencom/ore/x/systemprompt"`)
				assert.Contains(t, content, `app.WithTransform("systemprompt1",`)
				assert.Contains(t, content, `tr, err := systemprompt1.New()`)
				assert.Contains(t, content, `return tr.AsTransform(), nil`)
				assert.Contains(t, content, `return systemprompt.New()`)
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

func TestGenerateGoMod_Transforms(t *testing.T) {
	blueprint := &Blueprint{
		Dist:     Dist{Name: "test-agent", OutputPath: "./out"},
		Conduits: []ConduitConfig{{Module: "github.com/andrewhowdencom/ore/x/conduit/http"}},
		Transforms: []TransformConfig{
			{Module: "github.com/andrewhowdencom/ore/x/systemprompt"},
		},
	}

	got, err := GenerateGoMod(blueprint, "/absolute/path/to/ore")
	require.NoError(t, err)

	content := string(got)
	assert.Contains(t, content, "module test-agent")
	assert.Contains(t, content, `replace github.com/andrewhowdencom/ore/x/systemprompt => /absolute/path/to/ore/x/systemprompt`)
}
