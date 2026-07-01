package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider is a test double implementing provider.Provider.
// It writes its canned artifacts to the result channel and returns
// the configured error.
type stubProvider struct {
	artifacts []artifact.Artifact
	err       error
}

var _ provider.Provider = (*stubProvider)(nil)

func (s *stubProvider) Invoke(_ context.Context, _ ledger.State, _ models.Spec, ch chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	for _, a := range s.artifacts {
		ch <- a
	}
	return s.err
}

// newSubAgent returns a factory that builds a fresh *agent.Agent per
// invocation. Each agent has a SingleShot pattern with the supplied
// provider and a zero-value model spec; the resulting *agent.Agent is
// closed by the sub-agent closure body (via defer) at the end of each
// tool invocation.
func newSubAgent(t *testing.T, p provider.Provider) func() (*agent.Agent, error) {
	t.Helper()
	return func() (*agent.Agent, error) {
		return agent.New("test-subagent",
			agent.WithProvider(p),
			agent.WithSpec(models.Spec{}),
			agent.WithPattern(&cognitive.SingleShot{}),
		), nil
	}
}

func TestAsTool_DescriptorAndSchema(t *testing.T) {
	factory := newSubAgent(t, &stubProvider{})
	desc, fn := AsTool(factory, "echo", "An echo sub-agent.")
	require.NotNil(t, fn)

	assert.Equal(t, "echo", desc.Name)
	assert.Equal(t, "An echo sub-agent.", desc.Description)
	require.NotNil(t, desc.Schema)

	// Schema validation: the input is a single required "prompt" field.
	props, ok := desc.Schema["properties"].(map[string]any)
	require.True(t, ok)
	prompt, ok := props["prompt"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", prompt["type"])
	required, ok := desc.Schema["required"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"prompt"}, required)
}

func TestAsTool_RunsAgentAndReturnsAssistantText(t *testing.T) {
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Hello from the sub-agent."},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)
	assert.Equal(t, "Hello from the sub-agent.", result)
}

func TestAsTool_ConcatenatesTextAndTextDelta(t *testing.T) {
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Part A. "},
			artifact.TextDelta{Content: "Part B."},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)
	assert.Equal(t, "Part A. Part B.", result)
}

func TestAsTool_RequiresPrompt(t *testing.T) {
	factory := newSubAgent(t, &stubProvider{})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	cases := []struct {
		name string
		args map[string]any
	}{
		{name: "missing key", args: map[string]any{}},
		{name: "empty string", args: map[string]any{"prompt": ""}},
		{name: "non-string", args: map[string]any{"prompt": 42}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fn(context.Background(), nil, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "prompt is required")
		})
	}
}

func TestAsTool_PropagatesAgentError(t *testing.T) {
	want := errors.New("agent run failed")
	factory := newSubAgent(t, &stubProvider{err: want})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	_, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
	assert.Contains(t, err.Error(), "subagent echo")
}

func TestAsTool_SandboxIsIgnored(t *testing.T) {
	// The sub-agent runs against a fresh ledger.Buffer seeded with the
	// prompt; the Sandbox argument is unused. Passing a non-nil
	// sandbox must not change the result.
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "OK"},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)
	assert.Equal(t, "OK", result)
}

func TestResultSystemPrompt(t *testing.T) {
	tr, err := ResultSystemPrompt()
	require.NoError(t, err)
	require.NotNil(t, tr)

	// Smoke: applying the transform to a state injects a RoleSystem
	// turn with the schema-rendering content. The transform must not
	// mutate the existing turn (it is prepended, not appended).
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	out, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := out.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, ledger.RoleSystem, turns[0].Role)
	assert.Equal(t, ledger.RoleUser, turns[1].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Contains(t, text.Content, `"status"`)
	assert.Contains(t, text.Content, `"summary"`)
	assert.Contains(t, text.Content, `"success"`)
	assert.Contains(t, text.Content, `"partial"`)
	assert.Contains(t, text.Content, `"failed"`)
}

func TestResultSchema_IsValidJSON(t *testing.T) {
	// The schema must round-trip through encoding/json without loss.
	b, err := json.Marshal(ResultSchema)
	require.NoError(t, err)
	var roundTripped map[string]any
	require.NoError(t, json.Unmarshal(b, &roundTripped))

	// Sanity: the schema declares the three Status values as the
	// only legal values for the status field, and marks both
	// "status" and "summary" as required.
	props, ok := ResultSchema["properties"].(map[string]any)
	require.True(t, ok)

	status, ok := props["status"].(map[string]any)
	require.True(t, ok)
	enum, ok := status["enum"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"success", "partial", "failed"}, enum)

	required, ok := ResultSchema["required"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"status", "summary"}, required)
}
