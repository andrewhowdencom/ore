package subagent

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
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

func (s *stubProvider) Invoke(_ context.Context, _ state.State, _ models.Spec, ch chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	for _, a := range s.artifacts {
		ch <- a
	}
	return s.err
}

// newSubAgent builds a sub-agent for tests. The agent is closed when
// the test ends.
func newSubAgent(t *testing.T, p provider.Provider) *agent.Agent {
	t.Helper()
	a := agent.New("test-subagent",
		agent.WithProvider(p),
		agent.WithSpec(models.Spec{}),
		agent.WithPattern(&cognitive.SingleShot{}),
	)
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestAsTool_DescriptorAndSchema(t *testing.T) {
	a := newSubAgent(t, &stubProvider{})
	desc, fn := AsTool(a, "echo", "An echo sub-agent.")
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
	a := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Hello from the sub-agent."},
		},
	})
	_, fn := AsTool(a, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)
	assert.Equal(t, "Hello from the sub-agent.", result)
}

func TestAsTool_ConcatenatesTextAndTextDelta(t *testing.T) {
	a := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Part A. "},
			artifact.TextDelta{Content: "Part B."},
		},
	})
	_, fn := AsTool(a, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)
	assert.Equal(t, "Part A. Part B.", result)
}

func TestAsTool_RequiresPrompt(t *testing.T) {
	a := newSubAgent(t, &stubProvider{})
	_, fn := AsTool(a, "echo", "An echo sub-agent.")

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
	a := newSubAgent(t, &stubProvider{err: want})
	_, fn := AsTool(a, "echo", "An echo sub-agent.")

	_, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
	assert.Contains(t, err.Error(), "subagent echo")
}

func TestAsTool_SandboxIsIgnored(t *testing.T) {
	// The sub-agent runs against a fresh state.Buffer seeded with the
	// prompt; the Sandbox argument is unused. Passing a non-nil
	// sandbox must not change the result.
	a := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "OK"},
		},
	})
	_, fn := AsTool(a, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)
	assert.Equal(t, "OK", result)
}
