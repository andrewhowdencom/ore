package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew_RequiresAPIKey verifies that New returns an error when
// WithAPIKey is omitted. The skeleton implements the required-option
// contract from day one so callers cannot accidentally ship a provider
// that authenticates as the empty string.
func TestNew_RequiresAPIKey(t *testing.T) {
	t.Parallel()

	_, err := New(WithModel("claude-3-7-sonnet-latest"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiKey")
}

// TestNew_RequiresModel verifies that New returns an error when
// WithModel is omitted. Symmetric to TestNew_RequiresAPIKey: callers
// must explicitly name the model so the SDK does not silently default
// to anything.
func TestNew_RequiresModel(t *testing.T) {
	t.Parallel()

	_, err := New(WithAPIKey("test-key"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}

// TestNew_SucceedsWithRequiredOptions verifies the happy path: an
// API key and a model are sufficient to construct a Provider. The
// skeleton exposes this so the rest of the test suite can build
// against a Provider without a live server.
func TestNew_SucceedsWithRequiredOptions(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "claude-3-7-sonnet-latest", p.model)
	assert.False(t, p.isOpenRouter, "empty base URL is Anthropic native, not OpenRouter")
}

// TestNew_OpenRouterBaseURL verifies that configuring an OpenRouter
// base URL flips the resolved isOpenRouter flag, which drives the
// auth header dispatch in Task 8. The skeleton just stores the
// flag; the full auth-header resolution lands in Task 8.
func TestNew_OpenRouterBaseURL(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("anthropic/claude-3.7-sonnet:thinking"),
		WithBaseURL("https://openrouter.ai/api/v1"),
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.True(t, p.isOpenRouter)
}

// TestNew_AnthropicNativeBaseURL verifies that a non-OpenRouter base
// URL is treated as Anthropic native. The isOpenRouter flag is the
// only resolved state checked here; the full auth-header
// resolution lands in Task 8.
func TestNew_AnthropicNativeBaseURL(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithBaseURL("https://api.anthropic.com"),
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.False(t, p.isOpenRouter)
}

// TestInvoke_StubReturnsNil verifies the skeleton Invoke is callable
// and returns nil. Streaming behavior lands in Task 7; this is a
// compile-and-wire smoke test only.
func TestInvoke_StubReturnsNil(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 1)
	require.NoError(t, p.Invoke(t.Context(), mem, ch))
	close(ch)
	for range ch {
		// drain
	}
}

// TestIsOpenRouter_TableDriven exercises the host-detection helper
// with a small table of base URLs. The substring-match heuristic
// mirrors the openai module's; the table documents which inputs
// trigger which branch.
func TestIsOpenRouter_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"empty defaults to Anthropic native", "", false},
		{"anthropic native host", "https://api.anthropic.com", false},
		{"openrouter canonical", "https://openrouter.ai/api/v1", true},
		{"openrouter subdomain", "https://beta.openrouter.ai/api/v1", true},
		{"anthropic native path with v1", "https://api.anthropic.com/v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isOpenRouter(tt.baseURL))
		})
	}
}

// TestProviderSerialize_ReplaysThinkingBlocks verifies the read-side of
// the reasoning round-trip on the Anthropic wire. An assistant turn
// containing (Reasoning, Text, ReasoningSignature{anthropic, signature})
// must be reconstructed as a thinking block carrying the signature
// followed by a text block, in the original artifact order. The test
// also asserts that an OpenAI-style signature in the same turn is
// silently dropped (the Anthropic wire has no slot for it).
func TestProviderSerialize_ReplaysThinkingBlocks(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "what is 2+2?"})
	mem.Append(state.RoleAssistant,
		artifact.Reasoning{Content: "Let me think step by step."},
		artifact.ReasoningSignature{
			Provider: "anthropic",
			SubKind:  "signature",
			Data:     "sig-blob-abc",
		},
		artifact.Text{Content: "The answer is 4."},
		// OpenAI-encrypted signature; must be dropped on the
		// Anthropic wire.
		artifact.ReasoningSignature{
			Provider: "openai",
			SubKind:  "encrypted",
			Data:     "openai-encrypted-blob",
		},
	)

	got := p.serializeMessages(mem)
	require.Empty(t, got.system, "user turn should not produce system text")
	require.Len(t, got.messages, 2, "expected user + assistant only")

	// Marshal the assistant message and inspect its content blocks.
	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)

	// The SDK marshals MessageParam as a top-level object with
	// role/content. Decode to verify the order and shape.
	var asst struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string `json:"type"`
			Thinking  string `json:"thinking,omitempty"`
			Signature string `json:"signature,omitempty"`
			Text      string `json:"text,omitempty"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(asstBytes, &asst))
	assert.Equal(t, "assistant", asst.Role)
	require.Len(t, asst.Content, 2, "thinking block then text block")

	require.Equal(t, "thinking", asst.Content[0].Type)
	assert.Equal(t, "Let me think step by step.", asst.Content[0].Thinking)
	assert.Equal(t, "sig-blob-abc", asst.Content[0].Signature,
		"signature from ReasoningSignature{anthropic, signature} must be attached to the thinking block")

	require.Equal(t, "text", asst.Content[1].Type)
	assert.Equal(t, "The answer is 4.", asst.Content[1].Text)

	// The OpenAI-encrypted signature must not appear anywhere in the
	// assistant content array. The reasoning must be attached to the
	// thinking block that immediately precedes it; the redacted
	// subkind, if seen, would be dropped silently here.
	for _, blk := range asst.Content {
		if blk.Type == "redacted_thinking" {
			t.Fatalf("did not expect a redacted_thinking block; OpenAI signatures must be dropped")
		}
	}
}

// TestProviderSerialize_ReplaysRedactedThinking verifies that a
// ReasoningSignature{anthropic, redacted} is emitted as a standalone
// redacted_thinking block and does not consume or attach to a
// pending thinking block. Redacted blocks are self-contained: the
// encrypted payload travels inside the block, not as a sibling
// signature.
func TestProviderSerialize_ReplaysRedactedThinking(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "ok?"})
	mem.Append(state.RoleAssistant,
		artifact.Reasoning{Content: "thinking text"},
		artifact.ReasoningSignature{
			Provider: "anthropic",
			SubKind:  "redacted",
			Data:     "encrypted-blob",
		},
		artifact.Text{Content: "answer"},
	)

	got := p.serializeMessages(mem)
	require.Len(t, got.messages, 2)

	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)

	var asst struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Data string `json:"data,omitempty"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(asstBytes, &asst))
	require.Len(t, asst.Content, 3, "thinking, redacted_thinking, text")

	assert.Equal(t, "thinking", asst.Content[0].Type)
	assert.Equal(t, "redacted_thinking", asst.Content[1].Type)
	assert.Equal(t, "encrypted-blob", asst.Content[1].Data)
	assert.Equal(t, "text", asst.Content[2].Type)
}

// TestProviderSerialize_ReplaysToolUseAndToolResult verifies the
// assistant tool_use / user tool_result round-trip on the Anthropic
// wire. An assistant ToolCall must be serialized as a `tool_use`
// block; a follow-up RoleTool turn must be serialized as a user
// message containing a `tool_result` block.
func TestProviderSerialize_ReplaysToolUseAndToolResult(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "what is the weather?"})
	mem.Append(state.RoleAssistant, artifact.ToolCall{
		ID:        "toolu_abc",
		Name:      "get_weather",
		Arguments: `{"location":"sf"}`,
	})
	mem.Append(state.RoleTool, artifact.ToolResult{
		ToolCallID: "toolu_abc",
		Content:     "72F sunny",
	})

	got := p.serializeMessages(mem)
	require.Empty(t, got.system)
	require.Len(t, got.messages, 3, "user + assistant + tool-result user")

	// Assistant: tool_use block.
	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)
	var asst struct {
		Role    string `json:"role"`
		Content []struct {
			Type  string         `json:"type"`
			ID    string         `json:"id,omitempty"`
			Name  string         `json:"name,omitempty"`
			Input map[string]any `json:"input,omitempty"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(asstBytes, &asst))
	require.Len(t, asst.Content, 1)
	require.Equal(t, "tool_use", asst.Content[0].Type)
	assert.Equal(t, "toolu_abc", asst.Content[0].ID)
	assert.Equal(t, "get_weather", asst.Content[0].Name)
	assert.Equal(t, "sf", asst.Content[0].Input["location"])

	// Tool result: a user-role message with a tool_result block.
	toolBytes, err := json.Marshal(got.messages[2])
	require.NoError(t, err)
	var toolMsg struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id,omitempty"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
			IsError bool `json:"is_error,omitempty"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(toolBytes, &toolMsg))
	assert.Equal(t, "user", toolMsg.Role)
	require.Len(t, toolMsg.Content, 1)
	require.Equal(t, "tool_result", toolMsg.Content[0].Type)
	assert.Equal(t, "toolu_abc", toolMsg.Content[0].ToolUseID)
	require.Len(t, toolMsg.Content[0].Content, 1)
	assert.Equal(t, "text", toolMsg.Content[0].Content[0].Type)
	assert.Equal(t, "72F sunny", toolMsg.Content[0].Content[0].Text)
	assert.False(t, toolMsg.Content[0].IsError)
}

// TestProviderSerialize_SystemCollapsedToSystemField verifies that a
// RoleSystem turn is hoisted out of the messages slice and placed on
// the request-level `system` field. A subsequent user turn keeps its
// position in the messages list.
func TestProviderSerialize_SystemCollapsedToSystemField(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleSystem, artifact.Text{Content: "be terse."})
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	got := p.serializeMessages(mem)
	require.Len(t, got.system, 1, "system text hoisted to system field")
	assert.Equal(t, "be terse.", got.system[0].Text)
	require.Len(t, got.messages, 1)
	// The user message must NOT carry the system text.
	userBytes, err := json.Marshal(got.messages[0])
	require.NoError(t, err)
	assert.NotContains(t, string(userBytes), "be terse.")
}

// TestProviderSerialize_EmptySignatureOnStandaloneReasoning verifies
// that a Reasoning artifact with no following signature produces a
// thinking block with an empty signature. Anthropic accepts this on
// replay and the framework will accept a later signature via a
// subsequent ReasoningSignature artifact in another turn.
func TestProviderSerialize_EmptySignatureOnStandaloneReasoning(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})
	mem.Append(state.RoleAssistant,
		artifact.Reasoning{Content: "thinking..."},
		artifact.Text{Content: "answer"},
	)

	got := p.serializeMessages(mem)
	require.Len(t, got.messages, 2)
	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)

	var asst struct {
		Content []struct {
			Type      string `json:"type"`
			Thinking  string `json:"thinking,omitempty"`
			Signature string `json:"signature,omitempty"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(asstBytes, &asst))
	require.Len(t, asst.Content, 2)
	assert.Equal(t, "thinking", asst.Content[0].Type)
	assert.Equal(t, "thinking...", asst.Content[0].Thinking)
	// Signature is the zero value: empty string. The wire shape permits
	// this; the upstream will assign its own signature on the next
	// turn.
	assert.Equal(t, "", asst.Content[0].Signature)
}

// TestProviderSerialize_StandaloneSignatureEmitsEmptyThinking verifies
// that a ReasoningSignature arriving with no preceding Reasoning
// artifact still produces a thinking block to carry the signature.
// Anthropic rejects thinking signatures in isolation, so the
// serializer must synthesize an empty thinking block as the
// signature's carrier.
func TestProviderSerialize_StandaloneSignatureEmitsEmptyThinking(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})
	mem.Append(state.RoleAssistant,
		artifact.ReasoningSignature{
			Provider: "anthropic",
			SubKind:  "signature",
			Data:     "sig-only",
		},
		artifact.Text{Content: "answer"},
	)

	got := p.serializeMessages(mem)
	require.Len(t, got.messages, 2)
	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)

	var asst struct {
		Content []struct {
			Type      string `json:"type"`
			Thinking  string `json:"thinking,omitempty"`
			Signature string `json:"signature,omitempty"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(asstBytes, &asst))
	require.Len(t, asst.Content, 2)
	assert.Equal(t, "thinking", asst.Content[0].Type)
	assert.Equal(t, "", asst.Content[0].Thinking, "empty thinking text")
	assert.Equal(t, "sig-only", asst.Content[0].Signature)
	assert.Equal(t, "text", asst.Content[1].Type)
}

// TestProviderInvokeOptions_DefaultsAreUnset verifies the freshly
// constructed invokeOptions has all *Set flags at false. Invoke
// must consult these flags before deciding whether to override
// SDK-level defaults (e.g. temperature, max_tokens).
func TestProviderInvokeOptions_DefaultsAreUnset(t *testing.T) {
	t.Parallel()

	got := applyInvokeOptions()
	assert.False(t, got.temperatureSet)
	assert.False(t, got.maxTokensSet)
	assert.False(t, got.thinkingBudgetSet)
	assert.False(t, got.toolsSet)
}

// TestProviderInvokeOptions_FoldsAllKnownOptions verifies that each
// per-invocation option type is recognized and folded into the
// resolved invokeOptions.
func TestProviderInvokeOptions_FoldsAllKnownOptions(t *testing.T) {
	t.Parallel()

	got := applyInvokeOptions(
		WithTemperature(0.42),
		WithMaxTokens(2048),
		WithThinkingBudget(1024),
	)

	assert.True(t, got.temperatureSet)
	assert.InDelta(t, 0.42, got.temperature, 0.0001)
	assert.True(t, got.maxTokensSet)
	assert.Equal(t, int64(2048), got.maxTokens)
	assert.True(t, got.thinkingBudgetSet)
	assert.Equal(t, int64(1024), got.thinkingBudget)
}

// TestProviderInvokeOptions_ThinkingBudgetZeroIsSet verifies that
// zero is a meaningful value, not the zero-value of the bool flag.
// The flag must be true so Invoke knows the caller explicitly
// requested no thinking; this is what allows WithThinkingBudget(0)
// to act as a no-op (i.e., the SDK will not receive a thinking field).
func TestProviderInvokeOptions_ThinkingBudgetZeroIsSet(t *testing.T) {
	t.Parallel()

	got := applyInvokeOptions(WithThinkingBudget(0))
	assert.True(t, got.thinkingBudgetSet, "explicit zero must set the flag")
	assert.Equal(t, int64(0), got.thinkingBudget)
}

// TestProviderSerialize_HandlesJSONArgumentValue verifies the tool-use
// path when the artifact carries a structured `Value` rather than a
// pre-marshaled string. The SDK receives a structured `any` so the
// input serializes to nested JSON, not a quoted blob.
func TestProviderSerialize_HandlesJSONArgumentValue(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	type args struct {
		Location string `json:"location"`
		Unit     string `json:"unit"`
	}
	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "weather?"})
	mem.Append(state.RoleAssistant, artifact.ToolCall{
		ID:    "toolu_xyz",
		Name:  "get_weather",
		Value: args{Location: "sf", Unit: "f"},
	})

	got := p.serializeMessages(mem)
	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)
	var asst struct {
		Content []struct {
			Type  string         `json:"type"`
			Input map[string]any `json:"input,omitempty"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(asstBytes, &asst))
	require.Len(t, asst.Content, 1)
	require.Equal(t, "tool_use", asst.Content[0].Type)
	assert.Equal(t, "sf", asst.Content[0].Input["location"])
	assert.Equal(t, "f", asst.Content[0].Input["unit"])
}

// TestProviderSerialize_IsErrorTruePropagates verifies the
// tool_result block carries the is_error flag when the upstream
// tool reported a failure. This is the wire shape the SDK uses to
// surface tool failures back to the model.
func TestProviderSerialize_IsErrorTruePropagates(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "weather?"})
	mem.Append(state.RoleAssistant, artifact.ToolCall{
		ID:        "toolu_err",
		Name:      "get_weather",
		Arguments: `{}`,
	})
	mem.Append(state.RoleTool, artifact.ToolResult{
		ToolCallID: "toolu_err",
		Content:     "service unavailable",
		IsError:     true,
	})

	got := p.serializeMessages(mem)
	toolBytes, err := json.Marshal(got.messages[2])
	require.NoError(t, err)
	var toolMsg struct {
		Content []struct {
			Type    string `json:"type"`
			IsError bool   `json:"is_error,omitempty"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(toolBytes, &toolMsg))
	require.Len(t, toolMsg.Content, 1)
	assert.True(t, toolMsg.Content[0].IsError, "is_error must propagate")
}

// TestProviderSerialize_OrderPreservedWithinTurn verifies that
// interleaved Text/Reasoning blocks are emitted in the order they
// appeared in state, even when Reasoning follows Text. This is the
// case where the model emits "I'll think about this" mid-response
// after a previous sentence; the round-trip must preserve the
// interleaving.
func TestProviderSerialize_OrderPreservedWithinTurn(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "go"})
	mem.Append(state.RoleAssistant,
		artifact.Text{Content: "First sentence. "},
		artifact.Reasoning{Content: "I'm thinking about the second part."},
		artifact.ReasoningSignature{Provider: "anthropic", SubKind: "signature", Data: "sig1"},
		artifact.Text{Content: "Second sentence."},
	)

	got := p.serializeMessages(mem)
	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)

	// Use a permissive content shape to inspect types in order.
	var asst struct {
		Content []map[string]any `json:"content"`
	}
	require.NoError(t, json.Unmarshal(asstBytes, &asst))
	require.Len(t, asst.Content, 3)
	assert.Equal(t, "text", asst.Content[0]["type"])
	assert.Equal(t, "First sentence. ", asst.Content[0]["text"])
	assert.Equal(t, "thinking", asst.Content[1]["type"])
	assert.Equal(t, "I'm thinking about the second part.", asst.Content[1]["thinking"])
	assert.Equal(t, "sig1", asst.Content[1]["signature"])
	assert.Equal(t, "text", asst.Content[2]["type"])
	assert.Equal(t, "Second sentence.", asst.Content[2]["text"])
}

// TestProviderSerialize_RoleToolSkipsNonToolResultArtifacts verifies
// that a RoleTool turn containing non-ToolResult artifacts (e.g.,
// stray text) silently drops them. The Anthropic tool_result
// round-trip shape requires every artifact in a RoleTool turn to be
// a ToolResult; any other shape is a state construction error and
// should not crash the serializer.
func TestProviderSerialize_RoleToolSkipsNonToolResultArtifacts(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "go"})
	mem.Append(state.RoleAssistant, artifact.ToolCall{
		ID:        "toolu_only",
		Name:      "noop",
		Arguments: `{}`,
	})
	mem.Append(state.RoleTool,
		artifact.Text{Content: "stray text — should be ignored"},
		artifact.ToolResult{ToolCallID: "toolu_only", Content: "result"},
		artifact.Image{URL: "http://example.com/x.png"},
	)

	got := p.serializeMessages(mem)
	require.Len(t, got.messages, 3, "user + assistant + tool-result user")

	toolBytes, err := json.Marshal(got.messages[2])
	require.NoError(t, err)
	// Marshal the wire shape: user message with a single tool_result.
	var toolMsg struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id,omitempty"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(toolBytes, &toolMsg))
	assert.Equal(t, "user", toolMsg.Role)
	require.Len(t, toolMsg.Content, 1, "only the ToolResult artifact is forwarded")
	assert.Equal(t, "tool_result", toolMsg.Content[0].Type)
	assert.Equal(t, "toolu_only", toolMsg.Content[0].ToolUseID)
}

// TestProviderSerialize_DropsMixedSystemTurn verifies the conservative
// policy: a system turn containing any non-text artifact is dropped
// from the system field rather than mis-routed. The framework does
// not currently emit such turns, so this is a defensive check.
func TestProviderSerialize_DropsMixedSystemTurn(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	// Mixed-content system turn: text + image. Should be dropped.
	mem.Append(state.RoleSystem,
		artifact.Text{Content: "you are terse"},
		artifact.Image{URL: "http://example.com/logo.png"},
	)
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	got := p.serializeMessages(mem)
	assert.Empty(t, got.system, "mixed system turn must be dropped")
	require.Len(t, got.messages, 1)
	userBytes, err := json.Marshal(got.messages[0])
	require.NoError(t, err)
	assert.NotContains(t, string(userBytes), "you are terse")
}

// TestProviderSerialize_ConcatTextSeparatesWithNewline verifies the
// multi-Text user turn shape: a single newline is inserted between
// adjacent Text artifacts. This mirrors the openai module's
// behavior so user-visible rendering is consistent across providers.
func TestProviderSerialize_ConcatTextSeparatesWithNewline(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser,
		artifact.Text{Content: "first"},
		artifact.Text{Content: "second"},
		artifact.Text{Content: "third"},
	)

	got := p.serializeMessages(mem)
	require.Len(t, got.messages, 1)
	userBytes, err := json.Marshal(got.messages[0])
	require.NoError(t, err)
	assert.Contains(t, string(userBytes), "first\\nsecond\\nthird",
		"consecutive Text artifacts must be joined with a single newline")
}

// TestProviderSerialize_HandlesBadJSONArguments verifies the
// tool-use path when the artifact's Arguments field is set to a
// non-JSON string. The serializer falls back to the raw string
// rather than producing a wire-level error, so the upstream model
// can diagnose the bad input.
func TestProviderSerialize_HandlesBadJSONArguments(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "go"})
	mem.Append(state.RoleAssistant, artifact.ToolCall{
		ID:        "toolu_bad",
		Name:      "no_op",
		Arguments: "this is not json",
	})

	got := p.serializeMessages(mem)
	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)

	// Bad JSON should pass through as a quoted string in the input.
	var asst struct {
		Content []struct {
			Type  string `json:"type"`
			Input any    `json:"input"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(asstBytes, &asst))
	require.Len(t, asst.Content, 1)
	require.Equal(t, "tool_use", asst.Content[0].Type)
	// The wire JSON should contain the raw string (escaped).
	assert.Contains(t, string(asstBytes), "this is not json")
}

// TestProviderSerialize_EmptyAssistantTurnStillEmits verifies that
// an assistente turn with zero artifacts still produces a valid
// assistente message. The framework never emits such turns, but the
// defensive contract is that the wire shape remains valid: the role
// must always be set, even if the content array is empty or
// marshaled as null. The Anthropic Messages API requires `role` to
// be set; an empty content is acceptable because it preserves
// conversational ordering.
func TestProviderSerialize_EmptyAssistantTurnStillEmits(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})
	mem.Append(state.RoleAssistant) // no artifacts

	got := p.serializeMessages(mem)
	require.Len(t, got.messages, 2)
	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)

	// The role must be "assistant" regardless of content shape.
	// The content field is permitted to marshal as either an
	// empty array or null; the SDK leaves that decision to the
	// underlying encoder.
	var asst struct {
		Role string `json:"role"`
	}
	require.NoError(t, json.Unmarshal(asstBytes, &asst))
	assert.Equal(t, "assistant", asst.Role)
}

// TestProviderSerialize_AppliesPreTasksFixesRegressions is a
// regression-guard table of all the per-turn shapes the serializer
// supports, exercised in a single test to keep the regression set
// local. New cases should be added here rather than as scattered
// tests so the wire shape is visible at a glance.
func TestProviderSerialize_AppliesPreTasksFixesRegressions(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	// Single round-trip with every artifact type in play.
	mem := state.NewBuffer()
	mem.Append(state.RoleSystem, artifact.Text{Content: "be terse."})
	mem.Append(state.RoleUser, artifact.Text{Content: "weather?"})
	mem.Append(state.RoleAssistant,
		artifact.Reasoning{Content: "thinking..."},
		artifact.ReasoningSignature{Provider: "anthropic", SubKind: "signature", Data: "sig"},
		artifact.ToolCall{ID: "toolu_1", Name: "get_weather", Arguments: `{"x":1}`},
		artifact.Text{Content: "calling now"},
	)
	mem.Append(state.RoleTool, artifact.ToolResult{
		ToolCallID: "toolu_1",
		Content:     "result",
	})

	got := p.serializeMessages(mem)
	require.Len(t, got.system, 1, "system hoisted")
	assert.Equal(t, "be terse.", got.system[0].Text)

	require.Len(t, got.messages, 3, "user + assistant + tool-result user")

	// Spot-check the user message: no system text leaks in.
	userBytes, err := json.Marshal(got.messages[0])
	require.NoError(t, err)
	assert.NotContains(t, string(userBytes), "be terse.")
	assert.Contains(t, string(userBytes), "weather?")

	// Assistant message: thinking (with sig), tool_use, text — in
	// the order they were emitted.
	asstBytes, err := json.Marshal(got.messages[1])
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(asstBytes), `"thinking":"thinking..."`))
	assert.True(t, strings.Contains(string(asstBytes), `"signature":"sig"`))
	assert.True(t, strings.Contains(string(asstBytes), `"type":"tool_use"`))
	assert.True(t, strings.Contains(string(asstBytes), `"type":"text"`))
	// Reasoning ordering: thinking block must precede text block.
	thinkingIdx := strings.Index(string(asstBytes), `"thinking":"thinking..."`)
	textIdx := strings.Index(string(asstBytes), `"text":"calling now"`)
	assert.True(t, thinkingIdx < textIdx, "thinking block must precede text")

	// Tool-result user message: tool_result block carrying the
	// tool_use_id.
	toolBytes, err := json.Marshal(got.messages[2])
	require.NoError(t, err)
	assert.Contains(t, string(toolBytes), `"type":"tool_result"`)
	assert.Contains(t, string(toolBytes), `"tool_use_id":"toolu_1"`)
}
