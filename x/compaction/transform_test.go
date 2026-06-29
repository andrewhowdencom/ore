package compaction

import (
	"context"
	"strconv"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// markBoundaryAtEnd sets the boundary index on a buffer to point at its
// last turn. It is the helper used to seed test buffers without
// invoking the summarization LLM call.
func markBoundaryAtEnd(t *testing.T, buf *state.Buffer) {
	t.Helper()
	turns := buf.Turns()
	require.NotEmpty(t, turns, "cannot mark a boundary on an empty buffer")
	buf.Meta().Set(MetaKeyBoundaryIndex, strconv.Itoa(len(turns)-1))
}

// markBoundary sets the boundary index on a buffer to the given index.
func markBoundary(t *testing.T, buf *state.Buffer, idx int) {
	t.Helper()
	turns := buf.Turns()
	require.GreaterOrEqual(t, idx, 0)
	require.Less(t, idx, len(turns))
	buf.Meta().Set(MetaKeyBoundaryIndex, strconv.Itoa(idx))
}

// textTurnForTransform returns a state.Turn of the given role with a single
// artifact.Text — a stand-in for any non-compaction turn.
func textTurnForTransform(role state.Role, content string) state.Turn {
	return state.Turn{
		Role:      role,
		Artifacts: []artifact.Artifact{artifact.Text{Content: content}},
	}
}

func TestTransform_NoBoundary_Identity(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "hello"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "hi"})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	// Identity: the returned state is the base.
	assert.Same(t, state.State(buf), out)

	got := out.Turns()
	require.Len(t, got, 2)
	assert.Equal(t, "hello", got[0].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "hi", got[1].Artifacts[0].(artifact.Text).Content)
}

func TestTransform_EmptyBuffer_Identity(t *testing.T) {
	buf := &state.Buffer{}

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)
	assert.Same(t, state.State(buf), out)
	assert.Empty(t, out.Turns())
}

func TestTransform_NilState_NilReturned(t *testing.T) {
	tr := NewTransform()
	out, err := tr.Transform(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestTransform_BoundaryAtEnd_Identity(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "hello"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "hi"})
	// Append a system turn that will be the boundary.
	buf.Append(state.RoleSystem, artifact.Text{Content: "summary"})
	markBoundaryAtEnd(t, buf)

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	// Boundary is the latest turn; projecting from that index
	// returns the boundary turn alone. The result is a view,
	// not the base state.
	assert.NotSame(t, state.State(buf), out)
	got := out.Turns()
	require.Len(t, got, 1)
	text, ok := got[0].Artifacts[0].(artifact.Text)
	assert.True(t, ok)
	assert.Equal(t, "summary", text.Content)
}

func TestTransform_BoundaryInMiddle_ProjectsOnward(t *testing.T) {
	// Buffer: [user0, assistant0, user1, assistant1, boundary, user2, assistant2]
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a0"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a1"})
	boundaryIdx := len(buf.Turns())
	buf.Append(state.RoleSystem, artifact.Text{Content: "summary"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u2"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a2"})
	markBoundary(t, buf, boundaryIdx)

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 3)

	// First visible turn is the boundary itself.
	assert.Equal(t, "summary", got[0].Artifacts[0].(artifact.Text).Content)

	// Subsequent turns are preserved verbatim.
	assert.Equal(t, "u2", got[1].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "a2", got[2].Artifacts[0].(artifact.Text).Content)

	// Canonical buffer is untouched.
	assert.Len(t, buf.Turns(), 7)
}

func TestTransform_MultipleBoundaries_LatestWins(t *testing.T) {
	// Two boundary markers: the latest one absorbs everything older.
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	firstIdx := len(buf.Turns())
	buf.Append(state.RoleSystem, artifact.Text{Content: "first summary"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a1"})
	secondIdx := len(buf.Turns())
	buf.Append(state.RoleSystem, artifact.Text{Content: "second summary"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u2"})

	// Set the latest boundary (secondIdx is greater than firstIdx).
	markBoundary(t, buf, secondIdx)

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 2)

	// Only the latest boundary is visible.
	assert.Equal(t, "second summary", got[0].Artifacts[0].(artifact.Text).Content)

	// And only the turn after the latest boundary is exposed.
	assert.Equal(t, "u2", got[1].Artifacts[0].(artifact.Text).Content)

	// Confirm: writing the *first* boundary would project to a wider slice.
	markBoundary(t, buf, firstIdx)
	out2, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)
	got2 := out2.Turns()
	assert.Equal(t, "first summary", got2[0].Artifacts[0].(artifact.Text).Content)
}

func TestTransform_DefensiveCopy(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	boundaryIdx := len(buf.Turns())
	buf.Append(state.RoleSystem, artifact.Text{Content: "summary"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})
	markBoundary(t, buf, boundaryIdx)

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	first := out.Turns()
	second := out.Turns()

	// Mutating one returned slice must not affect the next call.
	first[0].Role = state.RoleAssistant
	assert.Equal(t, state.RoleSystem, second[0].Role, "subsequent Turns() must not be affected by prior mutations")
}

func TestTransform_Append_DelegatesToBase(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	boundaryIdx := len(buf.Turns())
	buf.Append(state.RoleSystem, artifact.Text{Content: "summary"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})
	markBoundary(t, buf, boundaryIdx)

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	// Append on the projected view delegates to the underlying
	// base buffer. After appending, the base has 4 turns.
	out.Append(state.RoleAssistant, artifact.Text{Content: "a1"})
	assert.Len(t, buf.Turns(), 4)
	assert.Equal(t, state.RoleAssistant, buf.Turns()[3].Role, "appended turn lands on the base buffer")

	// The projection itself is a static snapshot of the buffer at
	// Transform time — it does not retroactively pick up the new
	// turn. That is the contract of state.NewView, which is what
	// the transform uses; the transform is re-run on each LLM call,
	// so the next call gets a fresh projection that includes the
	// appended turn.
	got := out.Turns()
	require.Len(t, got, 2)
	assert.Equal(t, "summary", got[0].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "u1", got[1].Artifacts[0].(artifact.Text).Content)
}

func TestTransform_NonCompactionSystemTurn_NotTreatedAsBoundary(t *testing.T) {
	// A RoleSystem turn without a boundary metadata key should not
	// trigger the projection — even when there is text content.
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleSystem, artifact.Text{Content: "you are a helpful assistant"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	// Identity: the projection is the full buffer.
	assert.Same(t, state.State(buf), out)
	assert.Len(t, out.Turns(), 3)
}

func TestTransform_MalformedBoundary_FallsBackToIdentity(t *testing.T) {
	// A non-integer boundary value must not panic or produce a
	// corrupt projection; the transform falls back to identity.
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a0"})
	buf.Meta().Set(MetaKeyBoundaryIndex, "not-a-number")

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	assert.Same(t, state.State(buf), out)
	assert.Len(t, out.Turns(), 2)
}

func TestTransform_OutOfRangeBoundary_FallsBackToIdentity(t *testing.T) {
	// A boundary index that points past the buffer (e.g. after a
	// LoadTurns that truncated the buffer) must not panic. The
	// transform falls back to identity.
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Meta().Set(MetaKeyBoundaryIndex, "999")

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	assert.Same(t, state.State(buf), out)
	assert.Len(t, out.Turns(), 1)
}

// TestTransform_PrependedVirtualTurns_PreservedAfterProjection verifies that
// when the buffer is wrapped in a state.Prepend (as x/systemprompt and
// x/guardrails do), the virtual turns added by the prepend survive the
// projection. This is a regression test for the bug reported in
// https://github.com/andrewhowdencom/ore/issues/500 — without the fix,
// the projection wraps the slice in state.NewView and the prepend's
// virtual turns are silently dropped from the LLM-facing view.
//
// Reproduction shape:
//
//	buf:           [user, assistant, boundary, user]    (boundary marked via Meta)
//	prependView:   [sysprompt_virtual, ...buf.Turns()]
//	expected:      [sysprompt_virtual, boundary, user]
//	buggy actual:  [boundary, user]
func TestTransform_PrependedVirtualTurns_PreservedAfterProjection(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a0"})

	// Append the boundary turn and record the index *before* the next
	// append, so the projection slices from the summary turn onward.
	boundaryIdx := len(buf.Turns())
	buf.Append(state.RoleSystem, artifact.Text{Content: "compaction summary"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u3"})
	markBoundary(t, buf, boundaryIdx)

	// Wrap the buffer in state.Prepend — this is exactly what
	// x/systemprompt.Transform and x/guardrails.Transform return.
	prepended := state.Prepend(buf, []state.Turn{
		{
			Role: state.RoleSystem,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "you are a helpful assistant"},
			},
		},
	})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), prepended)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 3, "system prompt virtual turn must survive the projection; got %d turns", len(got))

	// First turn: the prepended system prompt.
	assert.Equal(t, state.RoleSystem, got[0].Role)
	gotSysprompt, ok := got[0].Artifacts[0].(artifact.Text)
	require.True(t, ok, "first turn must be a Text artifact")
	assert.Equal(t, "you are a helpful assistant", gotSysprompt.Content)

	// Second turn: the compaction summary.
	assert.Equal(t, state.RoleSystem, got[1].Role)
	gotSummary, ok := got[1].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "compaction summary", gotSummary.Content)

	// Third turn: the post-compaction user turn.
	assert.Equal(t, state.RoleUser, got[2].Role)
	gotU3, ok := got[2].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "u3", gotU3.Content)
}

// TestTransform_RealisticPipeline_SystemPrompt_Compaction_Guardrails
// mirrors the exact transform chain workshop wires
// (../workshop/internal/app/app.go):
//
//	loop.WithTransforms(sp, compaction.NewTransform(), gr)
//
// x/systemprompt and x/guardrails both produce a state.Prepend; the
// compaction transform sits between them. After compaction, the LLM
// must still see both virtual prepend layers (persona + safety rules)
// plus the compaction summary, in that order.
func TestTransform_RealisticPipeline_SystemPrompt_Compaction_Guardrails(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a0"})
	boundaryIdx := len(buf.Turns())
	buf.Append(state.RoleSystem, artifact.Text{Content: "summary"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})
	markBoundary(t, buf, boundaryIdx)

	// Mirror what x/systemprompt.Transform returns: a prependView
	// over the buffer with the system persona as the virtual turn.
	sp := state.Prepend(buf, []state.Turn{
		{
			Role:      state.RoleSystem,
			Artifacts: []artifact.Artifact{artifact.Text{Content: "system-persona"}},
		},
	})

	// Run the compaction transform — this is the line that, in the
	// buggy version, wraps the projected slice in a static View and
	// drops the prepend.
	tr := NewTransform()
	projected, err := tr.Transform(context.Background(), sp)
	require.NoError(t, err)

	// Mirror what x/guardrails.Transform returns: another prependView
	// over the projection with safety rules as virtual turns.
	withGuards := state.Prepend(projected, []state.Turn{
		{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{artifact.Text{Content: "be terse"}},
		},
		{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{artifact.Text{Content: "no profanity"}},
		},
	})

	got := withGuards.Turns()
	require.Len(t, got, 5,
		"guardrails (2) + system prompt (1) + summary (1) + post-turn (1) = 5; got %d", len(got))

	// Layer 1: guardrails (prepended last by x/guardrails).
	assert.Equal(t, "be terse", got[0].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "no profanity", got[1].Artifacts[0].(artifact.Text).Content)

	// Layer 2: the system prompt — must survive the compaction transform.
	assert.Equal(t, state.RoleSystem, got[2].Role)
	assert.Equal(t, "system-persona", got[2].Artifacts[0].(artifact.Text).Content)

	// Layer 3: the boundary.
	assert.Equal(t, "summary", got[3].Artifacts[0].(artifact.Text).Content)

	// Layer 4: the post-compaction user turn.
	assert.Equal(t, "u1", got[4].Artifacts[0].(artifact.Text).Content)
}

// TestTransform_PrependedVirtualTurns_NoBoundary_Identity confirms the
// non-buggy baseline: when there is no boundary, a prepend-wrapped
// state passes through unchanged. This is the regression guard
// against the fix accidentally re-introducing projection in the
// identity path.
func TestTransform_PrependedVirtualTurns_NoBoundary_Identity(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a0"})

	prepended := state.Prepend(buf, []state.Turn{
		{
			Role:      state.RoleSystem,
			Artifacts: []artifact.Artifact{artifact.Text{Content: "system-persona"}},
		},
	})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), prepended)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 3)
	assert.Equal(t, "system-persona", got[0].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "u0", got[1].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "a0", got[2].Artifacts[0].(artifact.Text).Content)
}

func TestReadBoundaryIndex(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantIdx int
		wantOk  bool
	}{
		{"unset", MetaKeyBoundaryIndex, "", 0, false},
		{"valid", MetaKeyBoundaryIndex, "5", 5, true},
		{"zero", MetaKeyBoundaryIndex, "0", 0, true},
		{"malformed", MetaKeyBoundaryIndex, "not-a-number", 0, false},
		{"empty string", MetaKeyBoundaryIndex, "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &state.Buffer{}
			if tt.value != "" || tt.key != "" {
				if tt.value != "" {
					buf.Meta().Set(tt.key, tt.value)
				}
			}
			gotIdx, gotOk := readBoundaryIndex(buf)
			assert.Equal(t, tt.wantIdx, gotIdx)
			assert.Equal(t, tt.wantOk, gotOk)
		})
	}
}
