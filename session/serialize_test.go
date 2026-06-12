package session

import (
	"fmt"
	"testing"
	"time"

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
		artifact.Text{Content: "hello"},
		artifact.ToolCall{ID: "call_1", Name: "add", Arguments: `{"a":1,"b":2}`},
		artifact.ToolResult{ToolCallID: "call_1", Content: "3", IsError: false},
		artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		artifact.Image{URL: "http://example.com/img.png"},
		artifact.Reasoning{Content: "Let me think..."},
	}

	data, err := marshalArtifacts(artifacts)
	require.NoError(t, err)

	got, err := unmarshalArtifacts(data)
	require.NoError(t, err)
	require.Len(t, got, len(artifacts))

	// Round-tripped artifacts are value types, not pointers. This is
	// the contract that issue #416 normalized: every consumer in the
	// framework uses value-form type assertions, so the round-trip
	// path must produce values too.
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
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}},
			},
		},
		{
			name: "system and user turns",
			turns: []state.Turn{
				{Role: state.RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "sys"}}},
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "usr"}}},
			},
		},
		{
			name: "assistant turn with multiple artifacts",
			turns: []state.Turn{
				{
					Role: state.RoleAssistant,
					Artifacts: []artifact.Artifact{
						artifact.Reasoning{Content: "thinking..."},
						artifact.ToolCall{ID: "call_1", Name: "add", Arguments: `{"a":1}`},
					},
				},
				{
					Role: state.RoleTool,
					Artifacts: []artifact.Artifact{
						artifact.ToolResult{ToolCallID: "call_1", Content: "result"},
					},
				},
			},
		},
		{
			name: "usage artifact",
			turns: []state.Turn{
				{
					Role:      state.RoleAssistant,
					Artifacts: []artifact.Artifact{artifact.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8}},
				},
			},
		},
		{
			name: "image artifact",
			turns: []state.Turn{
				{
					Role:      state.RoleUser,
					Artifacts: []artifact.Artifact{artifact.Image{URL: "http://example.com/img.png"}},
				},
			},
		},
		{
			name: "turn with zero artifacts",
			turns: []state.Turn{
				{Role: state.RoleSystem},
			},
		},
		{
			name: "turns with timestamps",
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}, Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)},
				{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hi"}}, Timestamp: time.Date(2026, 1, 1, 12, 0, 5, 0, time.UTC)},
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
				assert.Equal(t, want.Timestamp, got[i].Timestamp)
				require.Len(t, got[i].Artifacts, len(want.Artifacts))
				for j, wantArtifact := range want.Artifacts {
					assert.Equal(t, wantArtifact.Kind(), got[i].Artifacts[j].Kind())
					assert.Equal(t, wantArtifact, got[i].Artifacts[j])
				}
			}
		})
	}
}

func TestMarshalArtifacts_ToolCallWithValue(t *testing.T) {
	// ToolCall with a custom Value should marshal successfully and
	// preserve Arguments on round-trip. Value may become map[string]any
	// because the concrete type is lost during JSON serialization.
	artifacts := []artifact.Artifact{
		artifact.ToolCall{
			ID:        "call_1",
			Name:      "bash",
			Arguments: `{"command":"go test ./..."}`,
			Value:     struct{ Command string }{Command: "go test ./..."},
		},
	}

	data, err := marshalArtifacts(artifacts)
	require.NoError(t, err)

	got, err := unmarshalArtifacts(data)
	require.NoError(t, err)
	require.Len(t, got, 1)

	tc, ok := got[0].(artifact.ToolCall)
	require.True(t, ok, "round-tripped ToolCall should be a value, got %T", got[0])
	assert.Equal(t, "call_1", tc.ID)
	assert.Equal(t, "bash", tc.Name)
	assert.Equal(t, `{"command":"go test ./..."}`, tc.Arguments)

	// Value may be deserialized as map[string]any; it is not guaranteed to
	// preserve the concrete type.
	if tc.Value != nil {
		switch v := tc.Value.(type) {
		case map[string]any:
			assert.Equal(t, "go test ./...", v["Command"])
		case struct{ Command string }:
			assert.Equal(t, "go test ./...", v.Command)
		default:
			assert.Fail(t, "unexpected Value type: %T", v)
		}
	}
}

// TestUnmarshalArtifacts_ValueTyped locks down the contract introduced
// in issue #416: the round-trip path produces value-typed artifacts
// (artifact.Text, *not* *artifact.Text), so the in-memory and
// disk-loaded shapes are identical. Every consumer in the framework
// type-asserts as a value; the silent failure mode (a value-form
// assertion against a pointer artifact) was the deeper bug behind
// the byte-counter miscounts. This test exists to prevent regression
// of the upstream fix, independently of any consumer.
func TestUnmarshalArtifacts_ValueTyped(t *testing.T) {
	// Marshal a representative slice and check every element's
	// dynamic type.
	inputs := []artifact.Artifact{
		artifact.Text{Content: "t"},
		artifact.Reasoning{Content: "r"},
		artifact.ToolCall{ID: "1", Name: "t"},
		artifact.ToolResult{ToolCallID: "1", Content: "r"},
		artifact.Image{URL: "http://x"},
		artifact.Usage{PromptTokens: 1},
	}
	data, err := marshalArtifacts(inputs)
	require.NoError(t, err)

	got, err := unmarshalArtifacts(data)
	require.NoError(t, err)
	require.Len(t, got, len(inputs))

	for i, want := range inputs {
		assert.Equal(t, want.Kind(), got[i].Kind(),
			"kind at index %d should be preserved", i)
	}

	// Spot-check one of each: assert the dynamic type is the value
	// form. If any of these is a pointer, the contract is broken.
	typeCheck := []struct {
		idx  int
		want interface{}
	}{
		{0, artifact.Text{}},
		{1, artifact.Reasoning{}},
		{2, artifact.ToolCall{}},
		{3, artifact.ToolResult{}},
		{4, artifact.Image{}},
		{5, artifact.Usage{}},
	}
	for _, tc := range typeCheck {
		// reflect.TypeOf is the only way to assert the *dynamic* type
		// without consuming the value via a type assertion. The
		// reflect.TypeOf(got[idx]) should equal reflect.TypeOf(tc.want).
		gotType := fmt.Sprintf("%T", got[tc.idx])
		wantType := fmt.Sprintf("%T", tc.want)
		assert.Equal(t, wantType, gotType,
			"round-tripped artifact at index %d should be a value, not a pointer", tc.idx)
	}
}
