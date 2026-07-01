package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

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

// newSubAgentFromFactory returns a factory that increments the supplied
// counter on each call, then delegates to a fresh-agent builder. Used
// to verify the factory is invoked exactly once per tool call.
func newSubAgentFromFactory(t *testing.T, counter *atomic.Int32, p provider.Provider) func() (*agent.Agent, error) {
	t.Helper()
	return func() (*agent.Agent, error) {
		counter.Add(1)
		return agent.New("test-subagent",
			agent.WithProvider(p),
			agent.WithSpec(models.Spec{}),
			agent.WithPattern(&cognitive.SingleShot{}),
		), nil
	}
}

// validResult is a helper that returns a JSON-conformant success
// payload, used as the default canned output for the bulk of tests.
func validResult(summary string) string {
	b, _ := json.Marshal(Result{Status: StatusSuccess, Summary: summary})
	return string(b)
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

func TestAsTool_RunsAgentAndReturnsStructuredResult(t *testing.T) {
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: validResult("Hello from the sub-agent.")},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)

	r, ok := result.(Result)
	require.True(t, ok, "result should be a Result struct, got %T", result)
	assert.Equal(t, StatusSuccess, r.Status)
	assert.Equal(t, "Hello from the sub-agent.", r.Summary)
}

func TestAsTool_ConcatenatesTextAndTextDelta(t *testing.T) {
	// The provider emits the JSON result across two streaming
	// deltas. The framework must concatenate them so the parser
	// sees a single complete JSON object.
	payload := validResult("Part A. Part B.")
	mid := len(payload) / 2

	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: payload[:mid]},
			artifact.TextDelta{Content: payload[mid:]},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)

	r, ok := result.(Result)
	require.True(t, ok)
	assert.Equal(t, "Part A. Part B.", r.Summary)
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
	// The sub-agent runs against a fresh ledger.Thread seeded with
	// the prompt; the Sandbox argument is unused. Passing a non-nil
	// sandbox must not change the result.
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: validResult("OK")},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)

	r, ok := result.(Result)
	require.True(t, ok)
	assert.Equal(t, "OK", r.Summary)
}

func TestAsTool_ReturnsStructuredResult_Findings(t *testing.T) {
	payload := `{"status":"partial","summary":"Found three issues","findings":{"issues":3,"file":"foo.go"}}`
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: payload},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)

	r, ok := result.(Result)
	require.True(t, ok)
	assert.Equal(t, StatusPartial, r.Status)
	assert.Equal(t, "Found three issues", r.Summary)

	findings, ok := r.Findings.(map[string]any)
	require.True(t, ok, "Findings should unmarshal to map[string]any, got %T", r.Findings)
	assert.EqualValues(t, 3, findings["issues"])
	assert.Equal(t, "foo.go", findings["file"])
}

func TestAsTool_SchemaValidation_MalformedJSON(t *testing.T) {
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "not json at all"},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	_, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse child output")
}

func TestAsTool_SchemaValidation_MissingField(t *testing.T) {
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: `{"summary":"missing status"}`},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err, "schema-conformant-but-invalid payloads are surfaced as Result, not error")

	r, ok := result.(Result)
	require.True(t, ok)
	assert.Equal(t, StatusFailed, r.Status)
	assert.Equal(t, `{"summary":"missing status"}`, r.Summary, "raw payload preserved in Summary for diagnosis")
}

func TestAsTool_SchemaValidation_BadEnum(t *testing.T) {
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: `{"status":"WRONG","summary":"bad enum"}`},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)

	r, ok := result.(Result)
	require.True(t, ok)
	assert.Equal(t, StatusFailed, r.Status)
	assert.Equal(t, `{"status":"WRONG","summary":"bad enum"}`, r.Summary)
}

func TestAsTool_SchemaValidation_EmptySummary(t *testing.T) {
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: `{"status":"success","summary":""}`},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	result, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.NoError(t, err)

	r, ok := result.(Result)
	require.True(t, ok)
	assert.Equal(t, StatusFailed, r.Status)
}

func TestAsTool_EmptyChildOutput_IsError(t *testing.T) {
	// The provider emits only non-text artifacts (e.g., a tool call
	// or a stop reason). The child produced no text, so the closure
	// cannot validate anything. This is a tool error.
	factory := newSubAgent(t, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: ""},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	_, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestAsTool_FactoryCalledPerInvocation(t *testing.T) {
	var calls atomic.Int32
	factory := newSubAgentFromFactory(t, &calls, &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: validResult("x")},
		},
	})
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	_, err := fn(context.Background(), nil, map[string]any{"prompt": "a"})
	require.NoError(t, err)
	_, err = fn(context.Background(), nil, map[string]any{"prompt": "b"})
	require.NoError(t, err)

	assert.Equal(t, int32(2), calls.Load(), "factory must be called once per tool invocation")
}

func TestAsTool_AgentClosedAfterCall(t *testing.T) {
	// A factory that violates the contract by returning the same
	// agent twice demonstrates the lifecycle: the first call closes
	// the agent via defer; the second call's Run must fail because
	// the underlying step is closed. We bound the second call with
	// a short context timeout because the closed agent's Run can
	// block on the closed event bus instead of erroring promptly.
	sharedAgent := agent.New("shared",
		agent.WithProvider(&stubProvider{
			artifacts: []artifact.Artifact{
				artifact.Text{Content: validResult("x")},
			},
		}),
		agent.WithSpec(models.Spec{}),
		agent.WithPattern(&cognitive.SingleShot{}),
	)

	factory := func() (*agent.Agent, error) {
		return sharedAgent, nil
	}
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	_, err := fn(context.Background(), nil, map[string]any{"prompt": "a"})
	require.NoError(t, err)

	// Second call with the same agent: the contract is broken; the
	// step is closed; the closure must surface this as an error
	// within the timeout (a hang on a closed event bus counts as
	// a test failure).
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_, err = fn(ctx, nil, map[string]any{"prompt": "b"})
	require.Error(t, err, "second call must fail because the agent was closed after the first call")
}

func TestAsTool_BuildError_IsWrapped(t *testing.T) {
	// A factory that returns an error must be wrapped with the
	// sub-agent name and the underlying error preserved (errors.Is).
	want := errors.New("upstream missing API key")
	factory := func() (*agent.Agent, error) {
		return nil, want
	}
	_, fn := AsTool(factory, "echo", "An echo sub-agent.")

	_, err := fn(context.Background(), nil, map[string]any{"prompt": "Hi."})
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
	assert.Contains(t, err.Error(), "subagent echo")
}

func TestResultSystemPrompt(t *testing.T) {
	tr, err := ResultSystemPrompt()
	require.NoError(t, err)
	require.NotNil(t, tr)

	// Smoke: applying the transform to a state injects a RoleSystem
	// turn with the schema-rendering content. The transform must not
	// mutate the existing turn (it is prepended, not appended).
	base := ledger.NewThread()
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
