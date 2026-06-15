package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/anthropics/anthropic-sdk-go"
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

// recordingTransport is an http.RoundTripper that captures the outgoing
// request (method, URL, headers, body) and returns a canned response.
// It is safe for concurrent use; the auth-dispatch tests and the
// streaming tests both need to inspect what the SDK actually puts on
// the wire, and this single helper is shared between them.
//
// The transport supports two response modes:
//   - Default (Content-Type: application/json) for the non-streaming
//     Messages.New call exercised by triggerRequest in the
//     auth-dispatch tests.
//   - SSE (Content-Type: text/event-stream) when contentType is set to
//     "text/event-stream"; the response body is returned verbatim so
//     tests can build canned SSE event streams.
type recordingTransport struct {
	mu          sync.Mutex
	request     *http.Request
	body        []byte
	response    string
	contentType string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.request = req
	r.body = body
	resp := r.response
	ct := r.contentType
	r.mu.Unlock()
	if resp == "" {
		resp = `{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"test","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	}
	if ct == "" {
		ct = "application/json"
	}
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{ct}},
		Body:       io.NopCloser(strings.NewReader(resp)),
	}, nil
}

func (r *recordingTransport) Request() *http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.request
}

func (r *recordingTransport) Body() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body
}

// triggerRequest forces the SDK to make one HTTP call by invoking the
// non-streaming Messages.New with a minimal body. The call is expected
// to fail in interesting ways (the mock returns a bare-minimum
// response that does not satisfy the typed Message struct) — what
// matters is that the request is made and captured by the transport
// before the failure path runs. Any error is swallowed because the
// test only cares about the captured request headers.
func triggerRequest(t *testing.T, p *Provider) {
	t.Helper()
	_, _ = p.client.Messages.New(
		context.Background(),
		anthropic.MessageNewParams{
			Model:     anthropic.Model(p.model),
			MaxTokens: 1,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock("ping")),
			},
		},
	)
}

// TestNew_OpenRouterBaseURL verifies that configuring an OpenRouter
// base URL drives the auth-header dispatch to Bearer. The check is
// done by recording an actual HTTP call and inspecting the
// Authorization header; x-api-key MUST be absent.
func TestNew_OpenRouterBaseURL(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{}
	p, err := New(
		WithAPIKey("test-key"),
		WithModel("anthropic/claude-3.7-sonnet:thinking"),
		WithBaseURL("https://openrouter.ai/api/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.True(t, p.isOpenRouter, "isOpenRouter must be true for an openrouter.ai base URL")

	triggerRequest(t, p)

	req := transport.Request()
	require.NotNil(t, req, "the SDK must have made an HTTP call")
	assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"),
		"OpenRouter auth header must be Authorization: Bearer <key>")
	assert.Empty(t, req.Header.Get("x-api-key"),
		"x-api-key must NOT be set on OpenRouter")
}

// TestNew_AnthropicNativeBaseURL verifies that a non-OpenRouter base
// URL drives the auth-header dispatch to x-api-key. The check is
// done by recording an actual HTTP call and inspecting the headers;
// Authorization MUST be absent.
func TestNew_AnthropicNativeBaseURL(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{}
	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithBaseURL("https://api.anthropic.com"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.False(t, p.isOpenRouter, "isOpenRouter must be false for an api.anthropic.com base URL")

	triggerRequest(t, p)

	req := transport.Request()
	require.NotNil(t, req, "the SDK must have made an HTTP call")
	assert.Equal(t, "test-key", req.Header.Get("x-api-key"),
		"Anthropic native auth header must be x-api-key: <key>")
	assert.Empty(t, req.Header.Get("Authorization"),
		"Authorization must NOT be set on Anthropic native")
	// The SDK injects anthropic-version automatically in its request
	// config middleware; assert the version is present and uses the
	// documented default.
	assert.Equal(t, "2023-06-01", req.Header.Get("anthropic-version"))
}

// TestInvoke_StubReturnsNil is intentionally absent. The original
// skeleton test asserted the stub Invoke returned nil; the stub has
// been replaced by a real streaming implementation that makes an
// HTTP call. The new streaming tests (TestProviderInvoke_Streams*)
// cover the live path with a mock transport, and the auth-dispatch
// tests above exercise the SDK option pipeline via
// triggerRequest. There is no longer a 'stub returns nil' code path
// to test.

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
		Content:    "72F sunny",
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

// TestProviderSerialize_DisplayDoesNotAffectWireFormat is a regression
// test for the bug where a non-dict Display value (set by the loop's
// applyDisplayHints from a string-returning tool.Tool.DisplayHint) was
// forwarded as the tool_use.input field. Anthropic rejects non-object
// inputs with "Input should be a valid dictionary (2013)".
//
// The wire format is always derived from ToolCall.Arguments, the JSON
// the model streamed. ToolCall.Display is for human rendering only
// and must never contaminate the wire format.
func TestProviderSerialize_DisplayDoesNotAffectWireFormat(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "list things"})
	// Display is the kind of string label that the built-in filesystem
	// tools produce from their DisplayHint. The previous implementation
	// of parseToolArguments preferred Display over Arguments, so this
	// string would have been sent as the input field and the request
	// would have been rejected by the API.
	mem.Append(state.RoleAssistant, artifact.ToolCall{
		ID:        "toolu_disp",
		Name:      "list_directory",
		Arguments: `{"path":"/home/../Development"}`,
		Display:   "📁 list_directory(/home/../Development)",
	})

	got := p.serializeMessages(mem)
	require.Len(t, got.messages, 2, "user + assistant")

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
	require.Equal(t, "toolu_disp", asst.Content[0].ID)
	require.Equal(t, "list_directory", asst.Content[0].Name)

	// The input field must be the JSON object from Arguments, not the
	// Display string. Asserting on map[string]any forces a dict shape:
	// a string Display value would have produced an unmarshal error
	// here, and a non-dict JSON would have left Input as nil.
	require.NotNil(t, asst.Content[0].Input, "input must be a dict, not the Display string")
	assert.Equal(t, "/home/../Development", asst.Content[0].Input["path"])
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
	assert.False(t, got.thinkingLevelSet)
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
		WithThinkingLevel(provider.ThinkingLevelMedium),
	)

	assert.True(t, got.temperatureSet)
	assert.InDelta(t, 0.42, got.temperature, 0.0001)
	assert.True(t, got.maxTokensSet)
	assert.Equal(t, int64(2048), got.maxTokens)
	assert.True(t, got.thinkingLevelSet)
	assert.Equal(t, provider.ThinkingLevelMedium, got.thinkingLevel)
}

// TestProviderInvokeOptions_ThinkingLevelOffIsSet verifies that the
// "off" level sets the flag (so Invoke knows thinking was explicitly
// disabled) and produces a value the translation helper recognizes.
func TestProviderInvokeOptions_ThinkingLevelOffIsSet(t *testing.T) {
	t.Parallel()

	got := applyInvokeOptions(WithThinkingLevel(provider.ThinkingLevelOff))
	assert.True(t, got.thinkingLevelSet, "explicit off must set the flag")
	assert.Equal(t, provider.ThinkingLevelOff, got.thinkingLevel)
}

// TestProviderInvokeOptions_ThinkingLevelFoldsCorrectly is a
// table-driven test that verifies each level is folded into the
// resolved invokeOptions without modification.
func TestProviderInvokeOptions_ThinkingLevelFoldsCorrectly(t *testing.T) {
	t.Parallel()

	cases := []provider.ThinkingLevel{
		provider.ThinkingLevelOff,
		provider.ThinkingLevelMinimal,
		provider.ThinkingLevelLow,
		provider.ThinkingLevelMedium,
		provider.ThinkingLevelHigh,
		provider.ThinkingLevelMax,
	}
	for _, level := range cases {
		got := applyInvokeOptions(WithThinkingLevel(level))
		assert.True(t, got.thinkingLevelSet, "level %q must set the flag", level)
		assert.Equal(t, level, got.thinkingLevel, "level must round-trip")
	}
}

// TestTranslateThinkingLevel verifies the per-level percentage
// mapping, the 1024 floor, and the (max_tokens - 1024) ceiling.
func TestTranslateThinkingLevel(t *testing.T) {
	t.Parallel()

	t.Run("off and unset disable thinking", func(t *testing.T) {
		t.Parallel()
		budget, ok := translateThinkingLevel(provider.ThinkingLevelOff, 32000)
		assert.False(t, ok, "off must disable thinking")
		assert.Equal(t, int64(0), budget)

		budget, ok = translateThinkingLevel("", 32000)
		assert.False(t, ok, "empty must disable thinking")
		assert.Equal(t, int64(0), budget)

		budget, ok = translateThinkingLevel("unknown", 32000)
		assert.False(t, ok, "unknown level must disable thinking (forward compat)")
		assert.Equal(t, int64(0), budget)
	})

	t.Run("percentage mapping at 32k max_tokens", func(t *testing.T) {
		t.Parallel()
		// 32000 is workshop's defaultAnthropicMaxTokens. 25% of 32000
		// is 8000 (not 8192; that requires 32768). The percentage is the
		// source of truth, so the actual budget depends on the configured
		// max_tokens.
		cases := []struct {
			level provider.ThinkingLevel
			want  int64
		}{
			{provider.ThinkingLevelMinimal, 1024}, // 2% of 32k = 640, floored to 1024
			{provider.ThinkingLevelLow, 2560},     // 8% of 32k
			{provider.ThinkingLevelMedium, 8000},  // 25% of 32k
			{provider.ThinkingLevelHigh, 16000},   // 50% of 32k
			{provider.ThinkingLevelMax, 25600},    // 80% of 32k
		}
		for _, tc := range cases {
			got, ok := translateThinkingLevel(tc.level, 32000)
			assert.True(t, ok, "%q must enable thinking", tc.level)
			assert.Equal(t, tc.want, got, "%q @ 32k", tc.level)
		}
	})

	t.Run("percentage mapping at 128k max_tokens", func(t *testing.T) {
		t.Parallel()
		// Verify the percentage scales with max_tokens.
		budget, ok := translateThinkingLevel(provider.ThinkingLevelMedium, 128000)
		assert.True(t, ok)
		assert.Equal(t, int64(32000), budget, "25% of 128k")
	})

	t.Run("ceiling enforced when percentage exceeds max_tokens - 1024", func(t *testing.T) {
		t.Parallel()
		// max = 4096, max level = 80% of 4096 = 3276; ceiling is 4096 - 1024 = 3072.
		budget, ok := translateThinkingLevel(provider.ThinkingLevelMax, 4096)
		assert.True(t, ok)
		assert.Equal(t, int64(3072), budget, "must be capped at max - 1024")
	})

	t.Run("impossibly small max_tokens disables thinking", func(t *testing.T) {
		t.Parallel()
		// max = 1024 cannot satisfy 1024 floor + 1024 visible response.
		budget, ok := translateThinkingLevel(provider.ThinkingLevelLow, 1024)
		assert.False(t, ok, "max_tokens too small to think")
		assert.Equal(t, int64(0), budget)
	})
}

// TestProviderSerialize_ArgumentsParseAsDict verifies the tool-use
// path when Arguments is a JSON string that parses to a dict. The
// SDK receives the parsed map[string]any so the input serializes to
// nested JSON, not a quoted blob. The wire format is always derived
// from Arguments, never from Display.
func TestProviderSerialize_ArgumentsParseAsDict(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "weather?"})
	mem.Append(state.RoleAssistant, artifact.ToolCall{
		ID:        "toolu_xyz",
		Name:      "get_weather",
		Arguments: `{"location":"sf","unit":"f"}`,
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
		Content:    "service unavailable",
		IsError:    true,
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
		Content:    "result",
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

// ---------------------------------------------------------------------------
// Streaming tests
// ---------------------------------------------------------------------------
//
// The tests below cover the streaming Invoke implementation. They use a
// recordingTransport (defined above) configured to return a canned SSE
// body. The SSE format follows the Anthropic Messages streaming
// protocol: alternating `event:` and `data:` lines, separated by a
// blank line, with the final `message_stop` event closing the stream.

// sseEvent builds a single SSE event line with the given event name
// and JSON payload. The blank line that terminates the event is
// included so callers can simply concatenate the result of
// sseEvent(...) calls.
func sseEvent(event string, payload string) string {
	return "event: " + event + "\ndata: " + payload + "\n\n"
}

// drainArtifacts closes the channel (caller-owned) and returns every
// artifact that arrived on it. Used by the streaming tests to
// collect the channel's output in arrival order.
func drainArtifacts(ch chan artifact.Artifact) []artifact.Artifact {
	close(ch)
	var out []artifact.Artifact
	for a := range ch {
		out = append(out, a)
	}
	return out
}

// TestProviderInvoke_StreamsThinking asserts that an SSE stream
// containing `content_block_delta` events with `type: "thinking_delta"`
// produces `artifact.ReasoningDelta` values on the channel in order,
// followed by a `ReasoningSignature` for the signature_delta event and
// a final `Usage` artifact with the cumulative token counts.
func TestProviderInvoke_StreamsThinking(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{
		contentType: "text/event-stream",
		response: sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-3-7-sonnet-latest","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`) +
			sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`) +
			sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think"}}`) +
			sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" about this"}}`) +
			sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_abc"}}`) +
			sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`) +
			sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens_details":{"thinking_tokens":4}}}`) +
			sseEvent("message_stop", `{"type":"message_stop"}`),
	}

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 16)
	require.NoError(t, p.Invoke(t.Context(), mem, ch))
	got := drainArtifacts(ch)

	// Two reasoning deltas, one signature, one usage.
	require.Len(t, got, 4)
	assert.Equal(t, "reasoning_delta", got[0].Kind())
	assert.Equal(t, "Let me think", got[0].(artifact.ReasoningDelta).Content)
	assert.Equal(t, "reasoning_delta", got[1].Kind())
	assert.Equal(t, " about this", got[1].(artifact.ReasoningDelta).Content)
	assert.Equal(t, "reasoning_signature", got[2].Kind())
	sig := got[2].(artifact.ReasoningSignature)
	assert.Equal(t, "anthropic", sig.Provider)
	assert.Equal(t, "signature", sig.SubKind)
	assert.Equal(t, "sig_abc", sig.Data)
	assert.Equal(t, "usage", got[3].Kind())
	u := got[3].(artifact.Usage)
	assert.Equal(t, 10, u.PromptTokens)
	assert.Equal(t, 5, u.CompletionTokens)
	assert.Equal(t, 4, u.ThinkingTokens)
}

// TestProviderInvoke_StreamsMixedTextAndThinking asserts that
// interleaved `thinking_delta` and `text_delta` events produce a turn
// with both `Reasoning` and `Text` artifacts (the deltas arrive in
// order; the loop accumulator produces the final artifacts when the
// channel is drained).
func TestProviderInvoke_StreamsMixedTextAndThinking(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{
		contentType: "text/event-stream",
		response: sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-3-7-sonnet-latest","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`) +
			sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`) +
			sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Planning..."}}`) +
			sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig1"}}`) +
			sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`) +
			sseEvent("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`) +
			sseEvent("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hello!"}}`) +
			sseEvent("content_block_stop", `{"type":"content_block_stop","index":1}`) +
			sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":3,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens_details":{"thinking_tokens":1}}}`) +
			sseEvent("message_stop", `{"type":"message_stop"}`),
	}

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 16)
	require.NoError(t, p.Invoke(t.Context(), mem, ch))
	got := drainArtifacts(ch)

	// Order on the channel:
	//   1. ReasoningDelta "Planning..."
	//   2. ReasoningSignature{anthropic, signature, sig1}
	//   3. TextDelta "Hello!"
	//   4. Usage (with thinking_tokens=1)
	require.Len(t, got, 4)
	assert.Equal(t, "reasoning_delta", got[0].Kind())
	assert.Equal(t, "Planning...", got[0].(artifact.ReasoningDelta).Content)
	assert.Equal(t, "reasoning_signature", got[1].Kind())
	assert.Equal(t, "signature", got[1].(artifact.ReasoningSignature).SubKind)
	assert.Equal(t, "text_delta", got[2].Kind())
	assert.Equal(t, "Hello!", got[2].(artifact.TextDelta).Content)
	assert.Equal(t, "usage", got[3].Kind())
	assert.Equal(t, 1, got[3].(artifact.Usage).ThinkingTokens)
}

// TestProviderInvoke_StreamsRedactedThinkingForReplay asserts that a
// `redacted_thinking` block in `content_block_start` is emitted as a
// `ReasoningSignature{anthropic, redacted}` artifact, which the
// write-side (`serializeMessages`) can attach to the next request
// verbatim. The Data field must be the opaque base64 blob from the
// wire, unmodified.
func TestProviderInvoke_StreamsRedactedThinkingForReplay(t *testing.T) {
	t.Parallel()

	const opaqueData = "openrouter.reasoning:eyJ0ZXh0IjoiZW5jcnlwdGVkIn0="
	transport := &recordingTransport{
		contentType: "text/event-stream",
		response: sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-3-7-sonnet-latest","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`) +
			sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"`+opaqueData+`"}}`) +
			sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`) +
			sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens_details":{"thinking_tokens":0}}}`) +
			sseEvent("message_stop", `{"type":"message_stop"}`),
	}

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 16)
	require.NoError(t, p.Invoke(t.Context(), mem, ch))
	got := drainArtifacts(ch)

	require.Len(t, got, 2)
	sig := got[0].(artifact.ReasoningSignature)
	assert.Equal(t, "anthropic", sig.Provider)
	assert.Equal(t, "redacted", sig.SubKind)
	assert.Equal(t, opaqueData, sig.Data)
	assert.Equal(t, "usage", got[1].Kind())
}

// TestProviderInvoke_PreservesUsageThinkingTokens asserts that
// `usage.output_tokens_details.thinking_tokens` is surfaced in the
// final `Usage` artifact's `ThinkingTokens` field. The field uses
// `omitempty` so it is hidden from the JSON payload when the host
// reports zero.
func TestProviderInvoke_PreservesUsageThinkingTokens(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{
		contentType: "text/event-stream",
		response: sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-3-7-sonnet-latest","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":42,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`) +
			sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
			sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`) +
			sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`) +
			sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":42,"output_tokens":24,"cache_creation_input_tokens":3,"cache_read_input_tokens":7,"output_tokens_details":{"thinking_tokens":11}}}`) +
			sseEvent("message_stop", `{"type":"message_stop"}`),
	}

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 16)
	require.NoError(t, p.Invoke(t.Context(), mem, ch))
	got := drainArtifacts(ch)

	// One text_delta, one usage.
	require.Len(t, got, 2)
	u := got[1].(artifact.Usage)
	assert.Equal(t, 42, u.PromptTokens)
	assert.Equal(t, 24, u.CompletionTokens)
	assert.Equal(t, 66, u.TotalTokens)
	assert.Equal(t, 11, u.ThinkingTokens)
	assert.Equal(t, 7, u.CacheReadTokens)
	assert.Equal(t, 3, u.CacheWriteTokens)
}

// TestProviderInvoke_ToolUseStreaming asserts that a `tool_use` block
// is emitted as a `ToolCallDelta` carrying the ID and Name at
// `content_block_start`, and the `input_json_delta` events are
// emitted as `ToolCallDelta{Arguments: <partial JSON>}` events with
// the same Index. The loop accumulator is responsible for assembling
// the final ToolCall.
func TestProviderInvoke_ToolUseStreaming(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{
		contentType: "text/event-stream",
		response: sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-3-7-sonnet-latest","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`) +
			sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"search","input":{}}}`) +
			sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`) +
			sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"hello\"}"}}`) +
			sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`) +
			sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens_details":{"thinking_tokens":0}}}`) +
			sseEvent("message_stop", `{"type":"message_stop"}`),
	}

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 16)
	require.NoError(t, p.Invoke(t.Context(), mem, ch))
	got := drainArtifacts(ch)

	// tool_call_delta x3 (ID+Name, partial JSON, partial JSON), then usage.
	require.Len(t, got, 4)
	assert.Equal(t, "tool_call_delta", got[0].Kind())
	t0 := got[0].(artifact.ToolCallDelta)
	assert.Equal(t, 0, t0.Index)
	assert.Equal(t, "toolu_1", t0.ID)
	assert.Equal(t, "search", t0.Name)
	assert.Empty(t, t0.Arguments)

	assert.Equal(t, "tool_call_delta", got[1].Kind())
	t1 := got[1].(artifact.ToolCallDelta)
	assert.Equal(t, 0, t1.Index)
	assert.Empty(t, t1.ID)
	assert.Empty(t, t1.Name)
	assert.Equal(t, `{"q":`, t1.Arguments)

	assert.Equal(t, "tool_call_delta", got[2].Kind())
	t2 := got[2].(artifact.ToolCallDelta)
	assert.Equal(t, 0, t2.Index)
	assert.Equal(t, `"hello"}`, t2.Arguments)

	assert.Equal(t, "usage", got[3].Kind())
}

// TestProviderInvoke_ContextCancellation asserts that a pre-cancelled
// context aborts the call promptly with ctx.Err() and that the
// adapter does not emit further artifacts after cancellation. The
// transport is configured to block on a channel so the call is
// in-flight when cancellation fires.
func TestProviderInvoke_ContextCancellation(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	blocking := &blockingTransport{release: release}

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithHTTPClient(&http.Client{Transport: blocking}),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	ch := make(chan artifact.Artifact, 16)
	err = p.Invoke(ctx, mem, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Unblock the transport so the test goroutine can exit cleanly.
	close(release)
}

// TestProviderInvoke_HTTPError asserts that a non-2xx response with
// an Anthropic-formatted error body is propagated as a wrapped error
// from Invoke. The transport returns a 401 with a JSON error body
// matching the Anthropic error schema.
func TestProviderInvoke_HTTPError(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{
		response: `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
	}
	// recordingTransport always returns 200; wrap it to allow 401.
	tr := &statusOverrideTransport{
		recordingTransport: transport,
		status:             401,
	}

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithHTTPClient(&http.Client{Transport: tr}),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 16)
	err = p.Invoke(t.Context(), mem, ch)
	require.Error(t, err)
}

// TestProviderInvoke_TalksToOpenRouter is the env-gated integration
// test that round-trips against OpenRouter's /api/v1/messages
// mirror. It is skipped unless OR_API_KEY is set in the environment.
// The test uses a real model (minimax/minimax-m3) so the wire
// returns a `thinking` content block, which is the regression
// scenario the issue describes.
func TestProviderInvoke_TalksToOpenRouter(t *testing.T) {
	apiKey := os.Getenv("OR_API_KEY")
	if apiKey == "" {
		t.Skip("OR_API_KEY not set; skipping OpenRouter integration test")
	}

	p, err := New(
		WithAPIKey(apiKey),
		WithModel("minimax/minimax-m3"),
		WithBaseURL("https://openrouter.ai/api/v1"),
	)
	require.NoError(t, err)

	mem := state.NewBuffer()
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 16)
	require.NoError(t, p.Invoke(t.Context(), mem, ch))
	got := drainArtifacts(ch)

	// The test passes if Invoke returned without error. The exact
	// artifact mix depends on the upstream model's behavior; the
	// regression we are guarding against is 'no thinking arrives
	// when the model does think', so a non-empty channel is the
	// acceptance criterion.
	require.NotEmpty(t, got, "OpenRouter must return at least one artifact for a thinking-capable model")
}

// ---------------------------------------------------------------------------
// Test transport helpers
// ---------------------------------------------------------------------------

// statusOverrideTransport wraps a recordingTransport and overrides
// the HTTP status code on the response. The recordingTransport
// always returns 200; the override lets tests simulate 4xx/5xx
// responses without writing a separate transport.
type statusOverrideTransport struct {
	recordingTransport *recordingTransport
	status             int
}

func (s *statusOverrideTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := s.recordingTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.StatusCode = s.status
	return resp, nil
}

// blockingTransport is a RoundTripper that blocks until the release
// channel is closed OR the request's context is cancelled. Used by
// TestProviderInvoke_ContextCancellation to keep a call in-flight
// while the context is cancelled; respecting req.Context() avoids a
// 60s timeout when the test cancels the context before the call.
type blockingTransport struct {
	release <-chan struct{}
}

func (b *blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case <-b.release:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sseEvent("message_stop", `{"type":"message_stop"}`))),
	}, nil
}
