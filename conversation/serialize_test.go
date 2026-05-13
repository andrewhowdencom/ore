package conversation

import (
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalArtifacts_Empty(t *testing.T) {
	data, err := marshalArtifacts(nil)
	require.NoError(t, err)
	assert.Equal(t, "[]", string(data))
}

func TestMarshalArtifacts_DeltaRejection(t *testing.T) {
	tests := []struct {
		name string
		a    artifact.Artifact
	}{
		{"text_delta", artifact.TextDelta{Content: "delta"}},
		{"reasoning_delta", artifact.ReasoningDelta{Content: "delta"}},
		{"tool_call_delta", artifact.ToolCallDelta{ID: "1", Name: "foo"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := marshalArtifacts([]artifact.Artifact{tt.a})
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "delta artifact")
			assert.Contains(t, err.Error(), tt.a.Kind())
		})
	}
}

func TestMarshalArtifacts_AllTypes(t *testing.T) {
	artifacts := []artifact.Artifact{
		&artifact.Text{Content: "hello"},
		&artifact.ToolCall{ID: "call_1", Name: "add", Arguments: `{"a":1,"b":2}`},
		&artifact.ToolResult{ToolCallID: "call_1", Content: "3", IsError: false},
		&artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		&artifact.Image{URL: "http://example.com/img.png"},
		&artifact.Reasoning{Content: "Let me think..."},
	}

	data, err := marshalArtifacts(artifacts)
	require.NoError(t, err)

	got, err := unmarshalArtifacts(data)
	require.NoError(t, err)
	require.Len(t, got, len(artifacts))

	for i, want := range artifacts {
		assert.Equal(t, want.Kind(), got[i].Kind())
		assert.Equal(t, want, got[i])
	}
}

func TestUnmarshalArtifacts_UnknownKind(t *testing.T) {
	data := []byte(`[{"kind":"unknown_type","data":{}}]`)
	_, err := unmarshalArtifacts(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown artifact kind")
}

func TestMarshalTurns_Empty(t *testing.T) {
	data, err := marshalTurns(nil)
	require.NoError(t, err)
	assert.Equal(t, "[]", string(data))
}

func TestMarshalTurns_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		turns []state.Turn
	}{
		{
			name: "single user turn with text",
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{&artifact.Text{Content: "hello"}}},
			},
		},
		{
			name: "system and user turns",
			turns: []state.Turn{
				{Role: state.RoleSystem, Artifacts: []artifact.Artifact{&artifact.Text{Content: "sys"}}},
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{&artifact.Text{Content: "usr"}}},
			},
		},
		{
			name: "assistant turn with multiple artifacts",
			turns: []state.Turn{
				{
					Role: state.RoleAssistant,
					Artifacts: []artifact.Artifact{
						&artifact.Reasoning{Content: "thinking..."},
						&artifact.ToolCall{ID: "call_1", Name: "add", Arguments: `{"a":1}`},
					},
				},
				{
					Role: state.RoleTool,
					Artifacts: []artifact.Artifact{
						&artifact.ToolResult{ToolCallID: "call_1", Content: "result"},
					},
				},
			},
		},
		{
			name: "usage artifact",
			turns: []state.Turn{
				{
					Role:      state.RoleAssistant,
					Artifacts: []artifact.Artifact{&artifact.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8}},
				},
			},
		},
		{
			name: "image artifact",
			turns: []state.Turn{
				{
					Role:      state.RoleUser,
					Artifacts: []artifact.Artifact{&artifact.Image{URL: "http://example.com/img.png"}},
				},
			},
		},
		{
			name: "turn with zero artifacts",
			turns: []state.Turn{
				{Role: state.RoleSystem},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := marshalTurns(tt.turns)
			require.NoError(t, err)

			got, err := unmarshalTurns(data)
			require.NoError(t, err)
			require.Len(t, got, len(tt.turns))

			for i, want := range tt.turns {
				assert.Equal(t, want.Role, got[i].Role)
				require.Len(t, got[i].Artifacts, len(want.Artifacts))
				for j, wantArtifact := range want.Artifacts {
					assert.Equal(t, wantArtifact.Kind(), got[i].Artifacts[j].Kind())
					assert.Equal(t, wantArtifact, got[i].Artifacts[j])
				}
			}
		})
	}
}
