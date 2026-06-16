package artifact

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface satisfaction checks.
var _ Artifact = Text{}
var _ Artifact = ToolCall{}
var _ Artifact = ToolResult{}
var _ Artifact = Usage{}
var _ Artifact = Image{}
var _ Artifact = Reasoning{}
var _ Artifact = ReasoningSignature{}
var _ Artifact = StopReason{}

var _ LLMRenderer = (*mockLLMRenderer)(nil)
var _ MarkdownRenderer = (*mockMarkdownRenderer)(nil)

type mockLLMRenderer struct{}

func (m *mockLLMRenderer) MarshalLLM() string { return "llm" }

type mockMarkdownRenderer struct{}

func (m *mockMarkdownRenderer) MarshalMarkdown() string { return "markdown" }

var _ Delta = TextDelta{}
var _ Delta = ReasoningDelta{}
var _ Delta = ToolCallDelta{}

var _ Accumulable = TextDelta{}
var _ Accumulable = ReasoningDelta{}
var _ Accumulable = ToolCallDelta{}

func TestDeltaArtifacts(t *testing.T) {
	// Delta types should satisfy the Delta interface.
	assert.Implements(t, (*Delta)(nil), TextDelta{})
	assert.Implements(t, (*Delta)(nil), ReasoningDelta{})
	assert.Implements(t, (*Delta)(nil), ToolCallDelta{})

	// Non-delta types should NOT satisfy the Delta interface.
	assert.False(t, isDelta(Text{}))
	assert.False(t, isDelta(ToolCall{}))
	assert.False(t, isDelta(ToolResult{}))
	assert.False(t, isDelta(Usage{}))
	assert.False(t, isDelta(Image{}))
	assert.False(t, isDelta(Reasoning{}))
	assert.False(t, isDelta(ReasoningSignature{}))
}

func isDelta(a Artifact) bool {
	_, ok := a.(Delta)
	return ok
}

func TestArtifactKinds(t *testing.T) {
	tests := []struct {
		name string
		a    Artifact
		want string
	}{
		{"text", Text{Content: "hello"}, "text"},
		{"tool_call", ToolCall{Name: "foo", Arguments: "{}"}, "tool_call"},
		{"image", Image{URL: "http://example.com/img.png"}, "image"},
		{"tool_call", ToolCall{Name: "foo", Arguments: "{}"}, "tool_call"},
		{"tool_result", ToolResult{ToolCallID: "call_1", Content: "ok"}, "tool_result"},
		{"usage", Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, "usage"},
		{"image", Image{URL: "http://example.com/img.png"}, "image"},
		{"reasoning", Reasoning{Content: "Let me think..."}, "reasoning"},
		{"reasoning_signature", ReasoningSignature{Provider: "anthropic", SubKind: "signature", Data: "x"}, "reasoning_signature"},
		{"stop_reason", StopReason{Reason: StopReasonLength}, "stop_reason"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.a.Kind())
		})
	}
}

func TestAccumulableInterface(t *testing.T) {
	assert.Implements(t, (*Accumulable)(nil), TextDelta{})
	assert.Implements(t, (*Accumulable)(nil), ReasoningDelta{})
	assert.Implements(t, (*Accumulable)(nil), ToolCallDelta{})
}

func TestStopReason_MarshalJSON(t *testing.T) {
	tests := []struct {
		name   string
		reason StopReasonKind
		want   string
	}{
		{"stop", StopReasonStop, `{"kind":"stop_reason","reason":"stop"}`},
		{"length", StopReasonLength, `{"kind":"stop_reason","reason":"length"}`},
		{"tool_use", StopReasonToolUse, `{"kind":"stop_reason","reason":"tool_use"}`},
		{"refusal", StopReasonRefusal, `{"kind":"stop_reason","reason":"refusal"}`},
		{"other", StopReasonOther, `{"kind":"stop_reason","reason":"other"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := StopReason{Reason: tt.reason}
			assert.Equal(t, "stop_reason", sr.Kind())

			got, err := sr.MarshalJSON()
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))

			// Round-trip: decode and re-encode; the result must match
			// the original. This guards against a typo in the JSON
			// tags breaking the read-side in either direction.
			var decoded StopReason
			require.NoError(t, json.Unmarshal(got, &decoded))
			reEncoded, err := decoded.MarshalJSON()
			require.NoError(t, err)
			assert.JSONEq(t, string(got), string(reEncoded))
		})
	}
}

func TestToolResult_ValueField(t *testing.T) {
	tr := ToolResult{ToolCallID: "call_1", Content: "ok", Value: 42, IsError: false}
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.Equal(t, "ok", tr.Content)
	assert.Equal(t, 42, tr.Value)
	assert.False(t, tr.IsError)
}

func TestToolResult_LLMString(t *testing.T) {
	tests := []struct {
		name    string
		tr      ToolResult
		want    string
	}{
		{
			name: "LLMRenderer takes precedence",
			tr:   ToolResult{Value: &mockLLMRenderer{}, Content: "fallback"},
			want: "llm",
		},
		{
			name: "json.Marshal fallback for simple value",
			tr:   ToolResult{Value: "hello", Content: "fallback"},
			want: `"hello"`,
		},
		{
			name: "nil Value falls back to Content",
			tr:   ToolResult{Value: nil, Content: "fallback"},
			want: "fallback",
		},
		{
			name: "zero Value falls back to Content",
			tr:   ToolResult{Content: "fallback"},
			want: "fallback",
		},
		{
			name: "unserializable Value falls back to Content",
			tr:   ToolResult{Value: make(chan int), Content: "fallback"},
			want: "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.tr.LLMString())
		})
	}
}

func TestToolResult_MarkdownString(t *testing.T) {
	tests := []struct {
		name string
		tr   ToolResult
		want string
	}{
		{
			name: "MarkdownRenderer takes precedence",
			tr:   ToolResult{Value: &mockMarkdownRenderer{}, Content: "fallback"},
			want: "markdown",
		},
		{
			name: "json.Marshal fallback for simple value",
			tr:   ToolResult{Value: 42, Content: "fallback"},
			want: "```json\n42\n```",
		},
		{
			name: "nil Value falls back to Content",
			tr:   ToolResult{Value: nil, Content: "fallback"},
			want: "fallback",
		},
		{
			name: "zero Value falls back to Content",
			tr:   ToolResult{Content: "fallback"},
			want: "fallback",
		},
		{
			name: "unserializable Value falls back to Content",
			tr:   ToolResult{Value: make(chan int), Content: "fallback"},
			want: "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.tr.MarkdownString())
		})
	}
}

// errorLLMValue implements LLMRenderer for the error-content test.
type errorLLMValue struct{}

func (errorLLMValue) MarshalLLM() string { return "rendered-by-error-llm" }

// errorMDValue implements MarkdownRenderer for the error-content test.
type errorMDValue struct{}

func (errorMDValue) MarshalMarkdown() string { return "rendered-by-error-md" }

// TestToolResult_RenderersUseContentOnError pins the short-circuit
// in LLMString and MarkdownString: when IsError is true and Content
// is non-empty, the renderers on Value are bypassed and Content is
// returned verbatim. This is what makes the `**Error:** <err>` footer
// reach both audiences — the renderers on Value would otherwise
// re-marshal the partial result and drop the appended footer.
func TestToolResult_RenderersUseContentOnError(t *testing.T) {
	content := "partial body\n\n**Error:** boom"
	tr := ToolResult{
		ToolCallID: "call_1",
		Content:    content,
		Value:      errorLLMValue{}, // would otherwise produce "rendered-by-error-llm"
		IsError:    true,
	}

	assert.Equal(t, content, tr.LLMString(),
		"LLMString must return Content verbatim on error, "+
			"ignoring the LLMRenderer on Value")

	// MarkdownString is the human-facing view; it must also see the
	// same body + footer.
	assert.Equal(t, content, tr.MarkdownString(),
		"MarkdownString must return Content verbatim on error, "+
			"ignoring the MarkdownRenderer on Value")

	// Sanity-check: a MarkdownRenderer on Value is still respected
	// when IsError is false (this is the success path and is
	// unchanged by the fix).
	success := ToolResult{
		ToolCallID: "call_1",
		Content:    "fallback",
		Value:      errorMDValue{},
	}
	assert.Equal(t, "rendered-by-error-md", success.MarkdownString(),
		"MarkdownRenderer on Value is honoured on the success path")
}

func TestToolCall_DisplayField(t *testing.T) {
	tc := ToolCall{ID: "call_1", Name: "foo", Arguments: `{"x":1}`, Display: 42}
	assert.Equal(t, "call_1", tc.ID)
	assert.Equal(t, "foo", tc.Name)
	assert.Equal(t, `{"x":1}`, tc.Arguments)
	assert.Equal(t, 42, tc.Display)
}

// TestToolCall_LLMString asserts that LLMString returns the wire
// format (Arguments) verbatim, ignoring Display. The LLM sees the
// JSON object the model originally streamed; the display value is a
// human-rendering concern that must not contaminate the LLM-visible
// size estimate.
func TestToolCall_LLMString(t *testing.T) {
	tests := []struct {
		name string
		tc   ToolCall
		want string
	}{
		{
			name: "Display with LLMRenderer is ignored",
			tc:   ToolCall{Display: &mockLLMRenderer{}, Arguments: "fallback"},
			want: "fallback",
		},
		{
			name: "Display with string is ignored",
			tc:   ToolCall{Display: "hello", Arguments: `{"x":1}`},
			want: `{"x":1}`,
		},
		{
			name: "Display with arbitrary value is ignored",
			tc:   ToolCall{Display: 42, Arguments: "fallback"},
			want: "fallback",
		},
		{
			name: "nil Display returns Arguments",
			tc:   ToolCall{Display: nil, Arguments: "fallback"},
			want: "fallback",
		},
		{
			name: "zero Display returns Arguments",
			tc:   ToolCall{Arguments: "fallback"},
			want: "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.tc.LLMString())
		})
	}
}

func TestToolCall_MarkdownString(t *testing.T) {
	tests := []struct {
		name string
		tc   ToolCall
		want string
	}{
		{
			name: "MarkdownRenderer takes precedence",
			tc:   ToolCall{Display: &mockMarkdownRenderer{}, Arguments: "fallback"},
			want: "markdown",
		},
		{
			name: "string Display is returned as-is (no JSON quoting)",
			tc:   ToolCall{Display: "📁 list_directory(/path)", Arguments: "fallback"},
			want: "📁 list_directory(/path)",
		},
		{
			name: "json.Marshal fallback for non-string non-renderer value",
			tc:   ToolCall{Display: 42, Arguments: "fallback"},
			want: `42`,
		},
		{
			name: "json.Marshal fallback for structured value",
			tc:   ToolCall{Display: map[string]any{"a": 1}, Arguments: "fallback"},
			want: `{"a":1}`,
		},
		{
			name: "nil Display falls back to Arguments",
			tc:   ToolCall{Display: nil, Arguments: "fallback"},
			want: "fallback",
		},
		{
			name: "zero Display falls back to Arguments",
			tc:   ToolCall{Arguments: "fallback"},
			want: "fallback",
		},
		{
			name: "unserializable Display falls back to Arguments",
			tc:   ToolCall{Display: make(chan int), Arguments: "fallback"},
			want: "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.tc.MarkdownString())
		})
	}
}

func TestAccumulable_MergeInto_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		delta    Accumulable
		acc      Artifact
		expected Artifact
	}{
		{
			name:     "TextDelta seeds new Text when acc is nil",
			delta:    TextDelta{Content: "hello"},
			acc:      nil,
			expected: Text{Content: "hello"},
		},
		{
			name:     "TextDelta merges into existing Text",
			delta:    TextDelta{Content: " world"},
			acc:      Text{Content: "hello"},
			expected: Text{Content: "hello world"},
		},
		{
			name:     "ReasoningDelta seeds new Reasoning when acc is nil",
			delta:    ReasoningDelta{Content: "think"},
			acc:      nil,
			expected: Reasoning{Content: "think"},
		},
		{
			name:     "ReasoningDelta merges into existing Reasoning",
			delta:    ReasoningDelta{Content: " deeply"},
			acc:      Reasoning{Content: "think"},
			expected: Reasoning{Content: "think deeply"},
		},
		{
			name:     "ToolCallDelta seeds new ToolCall when acc is nil",
			delta:    ToolCallDelta{Index: 0, ID: "call_1", Name: "search", Arguments: "q"},
			acc:      nil,
			expected: ToolCall{ID: "call_1", Name: "search", Arguments: "q", Display: nil},
		},
		{
			name:     "ToolCallDelta concatenates Name and Arguments",
			delta:    ToolCallDelta{Index: 0, ID: "", Name: "calc", Arguments: "1+"},
			acc:      ToolCall{ID: "call_1", Name: "search", Arguments: "q", Display: nil},
			expected: ToolCall{ID: "call_1", Name: "searchcalc", Arguments: "q1+", Display: nil},
		},
		{
			name:     "ToolCallDelta latest-wins overwrites ID",
			delta:    ToolCallDelta{Index: 0, ID: "call_2", Name: "", Arguments: ""},
			acc:      ToolCall{ID: "call_1", Name: "search", Arguments: "q", Display: nil},
			expected: ToolCall{ID: "call_2", Name: "search", Arguments: "q", Display: nil},
		},
		{
			name:     "ToolCallDelta empty ID preserves existing ID",
			delta:    ToolCallDelta{Index: 0, ID: "", Name: "calc", Arguments: "1"},
			acc:      ToolCall{ID: "call_1", Name: "search", Arguments: "q", Display: nil},
			expected: ToolCall{ID: "call_1", Name: "searchcalc", Arguments: "q1", Display: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.delta.MergeInto(tt.acc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestText_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(Text{Content: "hello"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"text","content":"hello"}`, string(data))
}

func TestTextDelta_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(TextDelta{Content: "world"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"text_delta","content":"world"}`, string(data))
}

func TestReasoning_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(Reasoning{Content: "think"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"reasoning","content":"think"}`, string(data))
}

func TestReasoningDelta_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(ReasoningDelta{Content: "chunk"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"reasoning_delta","content":"chunk"}`, string(data))
}

func TestToolCall_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(ToolCall{ID: "1", Name: "calc", Arguments: `{"a":1}`})
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"tool_call","id":"1","name":"calc","arguments":"{\"a\":1}"}`, string(data))
}

func TestToolCall_MarshalJSON_WithDisplay(t *testing.T) {
	tc := ToolCall{ID: "1", Name: "calc", Arguments: `{"a":1}`, Display: map[string]interface{}{"result": 42}}
	data, err := json.Marshal(tc)
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"tool_call","id":"1","name":"calc","arguments":"{\"a\":1}","display":"{\"result\":42}"}`, string(data))
}

func TestToolCallDelta_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(ToolCallDelta{Index: 0, ID: "1", Name: "calc", Arguments: "args"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"tool_call_delta","id":"1","name":"calc","arguments":"args","index":0}`, string(data))
}

func TestToolResult_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(ToolResult{ToolCallID: "1", Content: "result", IsError: false})
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"tool_result","tool_call_id":"1","content":"result","is_error":false}`, string(data))
}

func TestToolResult_MarshalJSON_WithValue(t *testing.T) {
	tr := ToolResult{ToolCallID: "1", Content: "result", Value: map[string]interface{}{"data": "value"}, IsError: false}
	data, err := json.Marshal(tr)
	require.NoError(t, err)
	content, _ := json.Marshal(tr.MarkdownString())
	expected := fmt.Sprintf(`{"kind":"tool_result","tool_call_id":"1","content":%s,"is_error":false}`, string(content))
	assert.JSONEq(t, expected, string(data))
}

func TestUsage_MarshalJSON(t *testing.T) {
	t.Run("zero cache fields are omitted", func(t *testing.T) {
		data, err := json.Marshal(Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30})
		require.NoError(t, err)
		assert.JSONEq(t, `{"kind":"usage","prompt_tokens":10,"completion_tokens":20,"total_tokens":30}`, string(data))
	})

	t.Run("non-zero cache fields are emitted", func(t *testing.T) {
		data, err := json.Marshal(Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
			CacheReadTokens:  8,
			CacheWriteTokens: 2,
		})
		require.NoError(t, err)
		assert.JSONEq(t, `{"kind":"usage","prompt_tokens":10,"completion_tokens":20,"total_tokens":30,"cache_read_tokens":8,"cache_write_tokens":2}`, string(data))
	})

	t.Run("cache read only is emitted without write", func(t *testing.T) {
		data, err := json.Marshal(Usage{
			PromptTokens:    100,
			CompletionTokens: 50,
			TotalTokens:     150,
			CacheReadTokens: 42,
		})
		require.NoError(t, err)
		// cache_write_tokens is omitted because it is the zero value.
		assert.JSONEq(t, `{"kind":"usage","prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"cache_read_tokens":42}`, string(data))
	})

	t.Run("round trips through json marshal and unmarshal", func(t *testing.T) {
		original := Usage{
			PromptTokens:     1000,
			CompletionTokens: 250,
			TotalTokens:      1250,
			CacheReadTokens:  800,
			CacheWriteTokens: 50,
		}
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var roundTripped Usage
		require.NoError(t, json.Unmarshal(data, &roundTripped))
		assert.Equal(t, original, roundTripped)
	})
}

func TestImage_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(Image{URL: "https://example.com/img.png"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"image","url":"https://example.com/img.png"}`, string(data))
}

func TestReasoningSignature_RoundTrip(t *testing.T) {
	original := ReasoningSignature{
		Provider: "anthropic",
		SubKind:  "signature",
		Data:     "opaque-signature-bytes",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"kind":"reasoning_signature","provider":"anthropic","sub_kind":"signature","data":"opaque-signature-bytes"}`,
		string(data),
	)

	var roundTripped ReasoningSignature
	require.NoError(t, json.Unmarshal(data, &roundTripped))
	assert.Equal(t, original, roundTripped)
}

func TestReasoningSignature_AllKindValues(t *testing.T) {
	// The three documented SubKind values must each round-trip cleanly.
	// New values extend the set but should follow the same shape.
	tests := []struct {
		name     string
		sig      ReasoningSignature
		wantJSON string
	}{
		{
			name:     "anthropic signature",
			sig:      ReasoningSignature{Provider: "anthropic", SubKind: "signature", Data: "sig"},
			wantJSON: `{"kind":"reasoning_signature","provider":"anthropic","sub_kind":"signature","data":"sig"}`,
		},
		{
			name:     "anthropic redacted",
			sig:      ReasoningSignature{Provider: "anthropic", SubKind: "redacted", Data: "encrypted"},
			wantJSON: `{"kind":"reasoning_signature","provider":"anthropic","sub_kind":"redacted","data":"encrypted"}`,
		},
		{
			name:     "openai encrypted",
			sig:      ReasoningSignature{Provider: "openai", SubKind: "encrypted", Data: "blob"},
			wantJSON: `{"kind":"reasoning_signature","provider":"openai","sub_kind":"encrypted","data":"blob"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, "reasoning_signature", tt.sig.Kind())
			data, err := json.Marshal(tt.sig)
			require.NoError(t, err)
			assert.JSONEq(t, tt.wantJSON, string(data))
		})
	}
}

func TestUsage_ThinkingTokens_Omitempty(t *testing.T) {
	t.Run("zero thinking tokens are omitted", func(t *testing.T) {
		data, err := json.Marshal(Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30})
		require.NoError(t, err)
		assert.JSONEq(t,
			`{"kind":"usage","prompt_tokens":10,"completion_tokens":20,"total_tokens":30}`,
			string(data),
		)
	})

	t.Run("non-zero thinking tokens are emitted", func(t *testing.T) {
		data, err := json.Marshal(Usage{
			PromptTokens:     10,
			CompletionTokens: 100,
			TotalTokens:      110,
			ThinkingTokens:   42,
		})
		require.NoError(t, err)
		assert.JSONEq(t,
			`{"kind":"usage","prompt_tokens":10,"completion_tokens":100,"total_tokens":110,"thinking_tokens":42}`,
			string(data),
		)
	})

	t.Run("thinking tokens round trip through marshal and unmarshal", func(t *testing.T) {
		original := Usage{
			PromptTokens:     100,
			CompletionTokens: 250,
			TotalTokens:      350,
			ThinkingTokens:   80,
		}
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var roundTripped Usage
		require.NoError(t, json.Unmarshal(data, &roundTripped))
		assert.Equal(t, original, roundTripped)
	})
}
