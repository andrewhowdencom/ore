package http

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHTTPMarkdownValue is a test double that implements artifact.MarkdownRenderer.
type mockHTTPMarkdownValue struct {
	output string
}

func (m mockHTTPMarkdownValue) MarshalMarkdown() string {
	return m.output
}

func TestArtifactToJSON(t *testing.T) {
	tests := []struct {
		name     string
		art      artifact.Artifact
		wantKind string
		wantDTO  artifactJSON
	}{
		{
			name:     "text",
			art:      artifact.Text{Content: "hello"},
			wantKind: "text",
			wantDTO:  artifactJSON{Kind: "text", Content: "hello"},
		},
		{
			name:     "text_delta",
			art:      artifact.TextDelta{Content: "he"},
			wantKind: "text_delta",
			wantDTO:  artifactJSON{Kind: "text_delta", Content: "he"},
		},
		{
			name:     "reasoning",
			art:      artifact.Reasoning{Content: "think"},
			wantKind: "reasoning",
			wantDTO:  artifactJSON{Kind: "reasoning", Content: "think"},
		},
		{
			name:     "reasoning_delta",
			art:      artifact.ReasoningDelta{Content: "th"},
			wantKind: "reasoning_delta",
			wantDTO:  artifactJSON{Kind: "reasoning_delta", Content: "th"},
		},
		{
			name:     "tool_call",
			art:      artifact.ToolCall{ID: "1", Name: "calc", Arguments: `{"a":1}`},
			wantKind: "tool_call",
			wantDTO:  artifactJSON{Kind: "tool_call", ID: "1", Name: "calc", Arguments: `{"a":1}`},
		},
		{
			name:     "tool_call_delta",
			art:      artifact.ToolCallDelta{ID: "1", Name: "calc", Arguments: `{"`},
			wantKind: "tool_call_delta",
			wantDTO:  artifactJSON{Kind: "tool_call_delta", ID: "1", Name: "calc", Arguments: `{"`},
		},
		{
			name:     "tool_result",
			art:      artifact.ToolResult{ToolCallID: "1", Content: "42", IsError: true},
			wantKind: "tool_result",
			wantDTO:  artifactJSON{Kind: "tool_result", ToolCallID: "1", Content: "42", IsError: true},
		},
		{
			name:     "tool_result_markdown_renderer",
			art:      artifact.ToolResult{ToolCallID: "1", Content: `{"raw":"json"}`, Value: mockHTTPMarkdownValue{output: "# Custom Markdown"}},
			wantKind: "tool_result",
			wantDTO:  artifactJSON{Kind: "tool_result", ToolCallID: "1", Content: "# Custom Markdown", IsError: false},
		},
		{
			name:     "tool_result_json_fallback",
			art:      artifact.ToolResult{ToolCallID: "1", Content: "fallback", Value: "hello"},
			wantKind: "tool_result",
			wantDTO:  artifactJSON{Kind: "tool_result", ToolCallID: "1", Content: `"hello"`, IsError: false},
		},
		{
			name:     "usage",
			art:      artifact.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
			wantKind: "usage",
			wantDTO:  artifactJSON{Kind: "usage", PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		},
		{
			name:     "image",
			art:      artifact.Image{URL: "http://example.com/img.png"},
			wantKind: "image",
			wantDTO:  artifactJSON{Kind: "image", URL: "http://example.com/img.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := artifactToJSON(tt.art)
			require.True(t, ok)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantDTO, *got)
			assert.Equal(t, tt.wantKind, tt.art.Kind())
		})
	}
}

func TestArtifactToJSON_Unsupported(t *testing.T) {
	// A custom artifact type not known to the serializer is skipped.
	_, ok := artifactToJSON(&unknownArtifact{})
	assert.False(t, ok)
}

type unknownArtifact struct{}

func (u *unknownArtifact) Kind() string { return "unknown" }

func TestMarshalArtifact(t *testing.T) {
	tests := []struct {
		name    string
		art     artifact.Artifact
		want    string
		wantErr bool
	}{
		{
			name: "text",
			art:  artifact.Text{Content: "hello"},
			want: `{"kind":"text","content":"hello"}`,
		},
		{
			name: "tool_result",
			art:  artifact.ToolResult{ToolCallID: "1", Content: "42", IsError: true},
			want: `{"kind":"tool_result","tool_call_id":"1","content":"42","is_error":true}`,
		},
		{
			name: "tool_result_markdown_renderer",
			art:  artifact.ToolResult{ToolCallID: "1", Content: `{"raw":"json"}`, Value: mockHTTPMarkdownValue{output: "# Markdown Result"}},
			want: `{"kind":"tool_result","tool_call_id":"1","content":"# Markdown Result"}`,
		},
		{
			name: "tool_result_json_fallback",
			art:  artifact.ToolResult{ToolCallID: "1", Content: "fallback", Value: 42},
			want: `{"kind":"tool_result","tool_call_id":"1","content":"42"}`,
		},
		{
			name: "unsupported",
			art:  &unknownArtifact{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalArtifact(tt.art)
			require.NoError(t, err)
			if tt.want == "" {
				assert.Nil(t, got)
				return
			}
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestUnmarshalArtifact(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    artifact.Artifact
		wantErr bool
	}{
		{
			name:  "text",
			input: `{"kind":"text","content":"hello"}`,
			want:  artifact.Text{Content: "hello"},
		},
		{
			name:  "text_delta",
			input: `{"kind":"text_delta","content":"he"}`,
			want:  artifact.TextDelta{Content: "he"},
		},
		{
			name:  "reasoning",
			input: `{"kind":"reasoning","content":"think"}`,
			want:  artifact.Reasoning{Content: "think"},
		},
		{
			name:  "reasoning_delta",
			input: `{"kind":"reasoning_delta","content":"th"}`,
			want:  artifact.ReasoningDelta{Content: "th"},
		},
		{
			name:  "tool_call",
			input: `{"kind":"tool_call","id":"1","name":"calc","arguments":"{\"a\":1}"}`,
			want:  artifact.ToolCall{ID: "1", Name: "calc", Arguments: `{"a":1}`},
		},
		{
			name:  "tool_call_delta",
			input: `{"kind":"tool_call_delta","id":"1","name":"calc","arguments":"{\""}`,
			want:  artifact.ToolCallDelta{ID: "1", Name: "calc", Arguments: `{"`},
		},
		{
			name:  "tool_result",
			input: `{"kind":"tool_result","tool_call_id":"1","content":"42","is_error":true}`,
			want:  artifact.ToolResult{ToolCallID: "1", Content: "42", IsError: true},
		},
		{
			name:  "usage",
			input: `{"kind":"usage","prompt_tokens":10,"completion_tokens":20,"total_tokens":30}`,
			want:  artifact.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		},
		{
			name:  "image",
			input: `{"kind":"image","url":"http://example.com/img.png"}`,
			want:  artifact.Image{URL: "http://example.com/img.png"},
		},
		{
			name:    "unsupported_kind",
			input:   `{"kind":"unknown"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalArtifact([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRoundTrip_Artifact(t *testing.T) {
	artifacts := []artifact.Artifact{
		artifact.Text{Content: "hello world"},
		artifact.TextDelta{Content: "he"},
		artifact.Reasoning{Content: "I should think about this"},
		artifact.ReasoningDelta{Content: "I sh"},
		artifact.ToolCall{ID: "tc-1", Name: "add", Arguments: `{"a":1,"b":2}`},
		artifact.ToolCallDelta{ID: "tc-1", Name: "add", Arguments: `{"a":1`},
		artifact.ToolResult{ToolCallID: "tc-1", Content: `3`, IsError: false},
		artifact.Usage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
		artifact.Image{URL: "https://example.com/cat.png"},
	}

	for _, art := range artifacts {
		t.Run(art.Kind(), func(t *testing.T) {
			data, err := MarshalArtifact(art)
			require.NoError(t, err)

			got, err := UnmarshalArtifact(data)
			require.NoError(t, err)
			assert.Equal(t, art, got)
		})
	}
}

func TestMarshalOutputEvent(t *testing.T) {
	tests := []struct {
		name    string
		event   loop.OutputEvent
		want    string
		wantErr bool
	}{
		{
			name:  "turn_complete",
			event: loop.TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hi"}}}},
			want:  `{"kind":"turn_complete","turn":{"role":"assistant","artifacts":[{"kind":"text","content":"hi"}]}}`,
		},
		{
			name:  "error",
			event: loop.ErrorEvent{Err: errors.New("boom")},
			want:  `{"kind":"error","message":"boom"}`,
		},
		{
			name:  "lifecycle_done",
			event: loop.LifecycleEvent{Phase: "done"},
			want:  `{"kind":"lifecycle","phase":"done"}`,
		},
		{
			name:  "text_artifact",
			event: loop.ArtifactEvent{Artifact: artifact.Text{Content: "hello"}},
			want:  `{"kind":"text","content":"hello"}`,
		},
		{
			name:  "text_delta_artifact",
			event: loop.ArtifactEvent{Artifact: artifact.TextDelta{Content: "he"}},
			want:  `{"kind":"text_delta","content":"he"}`,
		},
		{
			name:  "unsupported_artifact",
			event: loop.ArtifactEvent{Artifact: &unknownArtifact{}},
			want:  "",
		},
		{
			name:  "properties",
			event: loop.PropertiesEvent{Properties: map[string]string{"thread_id": "abc"}},
			want:  `{"kind":"properties","properties":{"thread_id":"abc"}}`,
		},
		{
			name:  "lifecycle",
			event: loop.LifecycleEvent{Phase: "submitted"},
			want:  `{"kind":"lifecycle","phase":"submitted"}`,
		},
		{
			name:  "lifecycle_with_context",
			event: loop.LifecycleEvent{Phase: "done", Ctx: loop.EventContext{Provenance: "http"}},
			want:  `{"kind":"lifecycle","phase":"done","context":{"provenance":"http"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalOutputEvent(tt.event)
			require.NoError(t, err)
			if tt.want == "" {
				assert.Nil(t, got)
				return
			}
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}



func TestTurnToJSON(t *testing.T) {
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello"},
			artifact.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		},
	}

	got, err := turnToJSON(turn)
	require.NoError(t, err)
	assert.Equal(t, "assistant", got.Role)
	assert.Len(t, got.Artifacts, 2)
	assert.Equal(t, artifactJSON{Kind: "text", Content: "hello"}, got.Artifacts[0])
	assert.Equal(t, artifactJSON{Kind: "usage", PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}, got.Artifacts[1])
}

func TestTurnToJSON_SkipsUnknownArtifact(t *testing.T) {
	turn := state.Turn{
		Role:      state.RoleAssistant,
		Artifacts: []artifact.Artifact{&unknownArtifact{}},
	}

	got, err := turnToJSON(turn)
	require.NoError(t, err)
	assert.Equal(t, "assistant", got.Role)
	assert.Empty(t, got.Artifacts)
}

func TestUnmarshalOutputEvent(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    loop.OutputEvent
		wantErr bool
	}{
		{
			name:  "turn_complete",
			input: `{"kind":"turn_complete","turn":{"role":"assistant","artifacts":[{"kind":"text","content":"hi"}]}}`,
			want:  loop.TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hi"}}}},
		},
		{
			name:  "error",
			input: `{"kind":"error","message":"boom"}`,
			want:  loop.ErrorEvent{Err: errors.New("boom")},
		},
		{
			name:  "lifecycle_done",
			input: `{"kind":"lifecycle","phase":"done"}`,
			want:  loop.LifecycleEvent{Phase: "done"},
		},
		{
			name:  "text_artifact",
			input: `{"kind":"text","content":"hello"}`,
			want:  loop.ArtifactEvent{Artifact: artifact.Text{Content: "hello"}},
		},
		{
			name:  "properties",
			input: `{"kind":"properties","properties":{"thread_id":"abc"}}`,
			want:  loop.PropertiesEvent{Properties: map[string]string{"thread_id": "abc"}},
		},
		{
			name:  "lifecycle",
			input: `{"kind":"lifecycle","phase":"submitted"}`,
			want:  loop.LifecycleEvent{Phase: "submitted"},
		},
		{
			name:  "lifecycle_with_context",
			input: `{"kind":"lifecycle","phase":"done","context":{"provenance":"http"}}`,
			want:  loop.LifecycleEvent{Phase: "done", Ctx: loop.EventContext{Provenance: "http"}},
		},
		{
			name:    "unknown_kind",
			input:   `{"kind":"something_else"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalOutputEvent([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRoundTrip_OutputEvent(t *testing.T) {
	events := []loop.OutputEvent{
		loop.TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}}},
		loop.ErrorEvent{Err: errors.New("something went wrong")},
		loop.LifecycleEvent{Phase: "done"},
		loop.ArtifactEvent{Artifact: artifact.Text{Content: "some text"}},
		loop.ArtifactEvent{Artifact: artifact.TextDelta{Content: "so"}},
		loop.ArtifactEvent{Artifact: artifact.ToolCall{ID: "1", Name: "calc", Arguments: `{"a":1}`}},
		loop.PropertiesEvent{Properties: map[string]string{"thread_id": "abc", "state": "ready"}},
		loop.LifecycleEvent{Phase: "submitted"},
		loop.LifecycleEvent{Phase: "done", Ctx: loop.EventContext{Provenance: "http"}},
	}

	for _, event := range events {
		t.Run(event.Kind(), func(t *testing.T) {
			data, err := MarshalOutputEvent(event)
			require.NoError(t, err)

			got, err := UnmarshalOutputEvent(data)
			require.NoError(t, err)
			assert.Equal(t, event, got)
		})
	}
}

func TestMarshalOutputEvent_WithContext(t *testing.T) {
	tests := []struct {
		name  string
		event loop.OutputEvent
		want  string
	}{
		{
			name:  "turn_complete_with_context",
			event: loop.TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}}, Ctx: loop.EventContext{Provenance: "http"}},
			want:  `{"kind":"turn_complete","turn":{"role":"assistant","artifacts":[{"kind":"text","content":"hello"}]},"context":{"provenance":"http"}}`,
		},
		{
			name:  "error_with_context",
			event: loop.ErrorEvent{Err: errors.New("boom"), Ctx: loop.EventContext{Provenance: "tui"}},
			want:  `{"kind":"error","message":"boom","context":{"provenance":"tui"}}`,
		},
		{
			name:  "lifecycle_done_with_context",
			event: loop.LifecycleEvent{Phase: "done", Ctx: loop.EventContext{Provenance: "http"}},
			want:  `{"kind":"lifecycle","phase":"done","context":{"provenance":"http"}}`,
		},
		{
			name:  "text_artifact_with_context",
			event: loop.ArtifactEvent{Artifact: artifact.Text{Content: "hello"}, Ctx: loop.EventContext{Provenance: "http"}},
			want:  `{"kind":"text","content":"hello","context":{"provenance":"http"}}`,
		},
		{
			name:  "status_with_context",
			event: loop.PropertiesEvent{Properties: map[string]string{"thread_id": "abc"}, Ctx: loop.EventContext{Provenance: "http"}},
			want:  `{"kind":"properties","properties":{"thread_id":"abc"},"context":{"provenance":"http"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalOutputEvent(tt.event)
			require.NoError(t, err)

			var gotMap, wantMap map[string]interface{}
			require.NoError(t, json.Unmarshal(got, &gotMap))
			require.NoError(t, json.Unmarshal([]byte(tt.want), &wantMap))
			assert.Equal(t, wantMap, gotMap)
		})
	}
}

func TestUnmarshalOutputEvent_WithContext(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  loop.OutputEvent
	}{
		{
			name:  "turn_complete_with_context",
			input: `{"kind":"turn_complete","turn":{"role":"assistant","artifacts":[{"kind":"text","content":"hello"}]},"context":{"provenance":"http"}}`,
			want:  loop.TurnCompleteEvent{Turn: state.Turn{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}}, Ctx: loop.EventContext{Provenance: "http"}},
		},
		{
			name:  "error_with_context",
			input: `{"kind":"error","message":"boom","context":{"provenance":"tui"}}`,
			want:  loop.ErrorEvent{Err: errors.New("boom"), Ctx: loop.EventContext{Provenance: "tui"}},
		},
		{
			name:  "lifecycle_done_with_context",
			input: `{"kind":"lifecycle","phase":"done","context":{"provenance":"http"}}`,
			want:  loop.LifecycleEvent{Phase: "done", Ctx: loop.EventContext{Provenance: "http"}},
		},
		{
			name:  "text_artifact_with_context",
			input: `{"kind":"text","content":"hello","context":{"provenance":"http"}}`,
			want:  loop.ArtifactEvent{Artifact: artifact.Text{Content: "hello"}, Ctx: loop.EventContext{Provenance: "http"}},
		},
		{
			name:  "status_with_context",
			input: `{"kind":"properties","properties":{"thread_id":"abc"},"context":{"provenance":"http"}}`,
			want:  loop.PropertiesEvent{Properties: map[string]string{"thread_id": "abc"}, Ctx: loop.EventContext{Provenance: "http"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalOutputEvent([]byte(tt.input))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMarshalOutputEvent_UnsupportedKind(t *testing.T) {
	_, err := MarshalOutputEvent(&bogusOutputEvent{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported event kind")
}

type bogusOutputEvent struct{}

func (b *bogusOutputEvent) Kind() string              { return "bogus" }
func (b *bogusOutputEvent) Context() loop.EventContext { return loop.EventContext{} }

func TestMarshalOutputEvent_OmitEmptyContext(t *testing.T) {
	data, err := MarshalOutputEvent(loop.TurnCompleteEvent{
		Turn: state.Turn{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}},
	})
	require.NoError(t, err)

	// Empty context should be omitted from JSON output
	assert.NotContains(t, string(data), "context")
	assert.NotContains(t, string(data), "provenance")
}

// customMarshalerEvent is a test-only OutputEvent that implements
// json.Marshaler, verifying the MarshalOutputEvent fallback path.
type customMarshalerEvent struct {
	Value string
	Ctx   loop.EventContext
}

func (c *customMarshalerEvent) Kind() string              { return "custom_marshaler" }
func (c *customMarshalerEvent) Context() loop.EventContext { return c.Ctx }
func (c *customMarshalerEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"kind":  c.Kind(),
		"value": c.Value,
	})
}

func TestMarshalOutputEvent_CustomMarshaler(t *testing.T) {
	event := &customMarshalerEvent{Value: "hello"}
	data, err := MarshalOutputEvent(event)
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"custom_marshaler","value":"hello"}`, string(data))
}

func TestArtifactJSON_ToolCallDelta_IndexRoundTrip(t *testing.T) {
	art := artifact.ToolCallDelta{Index: 2, ID: "tc-1", Name: "add", Arguments: "1"}
	data, err := MarshalArtifact(art)
	require.NoError(t, err)

	got, err := UnmarshalArtifact(data)
	require.NoError(t, err)
	td, ok := got.(artifact.ToolCallDelta)
	require.True(t, ok)
	assert.Equal(t, 2, td.Index)
	assert.Equal(t, "tc-1", td.ID)
	assert.Equal(t, "add", td.Name)
	assert.Equal(t, "1", td.Arguments)
}

func TestRoundTrip_PropertiesEvent(t *testing.T) {
	tests := []struct {
		name   string
		event  loop.PropertiesEvent
		want   string
	}{
		{
			name:  "empty_map",
			event: loop.PropertiesEvent{Properties: map[string]string{}},
			want:  `{"kind":"properties","properties":{}}`,
		},
		{
			name:  "single_key",
			event: loop.PropertiesEvent{Properties: map[string]string{"thread_id": "abc-123"}},
			want:  `{"kind":"properties","properties":{"thread_id":"abc-123"}}`,
		},
		{
			name:  "multiple_keys",
			event: loop.PropertiesEvent{Properties: map[string]string{"thread_id": "abc", "state": "thinking...", "model": "gpt-4o"}},
			want:  `{"kind":"properties","properties":{"thread_id":"abc","state":"thinking...","model":"gpt-4o"}}`,
		},
		{
			name:  "with_context",
			event: loop.PropertiesEvent{Properties: map[string]string{"thread_id": "abc"}, Ctx: loop.EventContext{Provenance: "http"}},
			want:  `{"kind":"properties","properties":{"thread_id":"abc"},"context":{"provenance":"http"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalOutputEvent(tt.event)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(data))

			got, err := UnmarshalOutputEvent(data)
			require.NoError(t, err)
			assert.Equal(t, tt.event, got)
		})
	}
}

func TestRoundTrip_LifecycleEvent(t *testing.T) {
	tests := []struct {
		name  string
		event loop.LifecycleEvent
		want  string
	}{
		{
			name:  "submitted",
			event: loop.LifecycleEvent{Phase: "submitted"},
			want:  `{"kind":"lifecycle","phase":"submitted"}`,
		},
		{
			name:  "streaming",
			event: loop.LifecycleEvent{Phase: "streaming"},
			want:  `{"kind":"lifecycle","phase":"streaming"}`,
		},
		{
			name:  "done",
			event: loop.LifecycleEvent{Phase: "done"},
			want:  `{"kind":"lifecycle","phase":"done"}`,
		},
		{
			name:  "with_context",
			event: loop.LifecycleEvent{Phase: "done", Ctx: loop.EventContext{Provenance: "http"}},
			want:  `{"kind":"lifecycle","phase":"done","context":{"provenance":"http"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalOutputEvent(tt.event)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(data))

			got, err := UnmarshalOutputEvent(data)
			require.NoError(t, err)
			assert.Equal(t, tt.event, got)
		})
	}
}
