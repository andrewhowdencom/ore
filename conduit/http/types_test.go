package http

import (
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			name:  "text_artifact",
			event: artifact.Text{Content: "hello"},
			want:  `{"kind":"text","content":"hello"}`,
		},
		{
			name:  "text_delta_artifact",
			event: artifact.TextDelta{Content: "he"},
			want:  `{"kind":"text_delta","content":"he"}`,
		},
		{
			name:  "unsupported_artifact",
			event: &unknownOutputEvent{},
			want:  "",
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

type unknownOutputEvent struct{}

func (u *unknownOutputEvent) Kind() string { return "unknown_event" }

func TestMarshalCompleteEvent(t *testing.T) {
	turns := []state.Turn{
		{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}},
		{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hi"}}},
	}

	got, err := MarshalCompleteEvent(turns)
	require.NoError(t, err)

	want := `{"kind":"complete","turns":[{"role":"user","artifacts":[{"kind":"text","content":"hello"}]},{"role":"assistant","artifacts":[{"kind":"text","content":"hi"}]}]}`
	assert.JSONEq(t, want, string(got))
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
			name:  "text_artifact",
			input: `{"kind":"text","content":"hello"}`,
			want:  artifact.Text{Content: "hello"},
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
		artifact.Text{Content: "some text"},
		artifact.TextDelta{Content: "so"},
		artifact.ToolCall{ID: "1", Name: "calc", Arguments: `{"a":1}`},
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
