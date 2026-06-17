package compaction

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compactionTurn returns a state.Turn of role RoleSystem carrying an
// artifact.Compaction. It is the helper used to seed test buffers
// without invoking the summarization LLM call.
func compactionTurn(idx int) state.Turn {
	return state.Turn{
		Role: state.RoleSystem,
		Artifacts: []artifact.Artifact{
			artifact.Compaction{
				CompactedThrough:     idx,
				DroppedTurnCount:     idx,
				DroppedTokenEstimate: 100,
				Strategy:             "summarize",
			},
		},
	}
}

// textTurn returns a state.Turn of the given role with a single
// artifact.Text — a stand-in for any non-compaction turn.
func textTurnForTransform(role state.Role, content string) state.Turn {
	return state.Turn{
		Role:      role,
		Artifacts: []artifact.Artifact{artifact.Text{Content: content}},
	}
}

func TestTransform_NoCompaction_Identity(t *testing.T) {
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

func TestTransform_CompactionAtEnd_Identity(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "hello"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "hi"})
	buf.Append(state.RoleSystem, artifact.Compaction{Strategy: "summarize"})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	// Compaction is the latest turn; projecting from index 2
	// returns the compaction turn alone. The result is a view,
	// not the base state.
	assert.NotSame(t, state.State(buf), out)
	got := out.Turns()
	require.Len(t, got, 1)
	_, ok := got[0].Artifacts[0].(artifact.Compaction)
	assert.True(t, ok)
}

func TestTransform_CompactionInMiddle_ProjectsOnward(t *testing.T) {
	// Buffer: [user0, assistant0, user1, assistant1, COMPACTION, user2, assistant2]
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a0"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a1"})
	buf.Append(state.RoleSystem, artifact.Compaction{
		Strategy:         "summarize",
		DroppedTurnCount: 4,
	})
	buf.Append(state.RoleUser, artifact.Text{Content: "u2"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a2"})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 3)

	// First visible turn is the compaction marker itself.
	_, ok := got[0].Artifacts[0].(artifact.Compaction)
	assert.True(t, ok)

	// Subsequent turns are preserved verbatim.
	assert.Equal(t, "u2", got[1].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "a2", got[2].Artifacts[0].(artifact.Text).Content)

	// Canonical buffer is untouched.
	assert.Len(t, buf.Turns(), 7)
}

func TestTransform_MultipleCompactions_LatestWins(t *testing.T) {
	// Two compactions: the latest one absorbs everything older.
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleSystem, artifact.Compaction{Strategy: "summarize", DroppedTurnCount: 1})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})
	buf.Append(state.RoleAssistant, artifact.Text{Content: "a1"})
	buf.Append(state.RoleSystem, artifact.Compaction{Strategy: "summarize", DroppedTurnCount: 3})
	buf.Append(state.RoleUser, artifact.Text{Content: "u2"})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 2)

	// Only the latest compaction is visible.
	c, ok := got[0].Artifacts[0].(artifact.Compaction)
	require.True(t, ok)
	assert.Equal(t, 3, c.DroppedTurnCount, "latest compaction's metadata is what is exposed")

	// And only the turn after the latest compaction is exposed.
	assert.Equal(t, "u2", got[1].Artifacts[0].(artifact.Text).Content)
}

func TestTransform_DefensiveCopy(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleSystem, artifact.Compaction{Strategy: "summarize"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})

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
	buf.Append(state.RoleSystem, artifact.Compaction{Strategy: "summarize"})
	buf.Append(state.RoleUser, artifact.Text{Content: "u1"})

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
	_, ok := got[0].Artifacts[0].(artifact.Compaction)
	assert.True(t, ok)
	assert.Equal(t, "u1", got[1].Artifacts[0].(artifact.Text).Content)
}

func TestTransform_NonCompactionSystemTurn_NotTreatedAsMarker(t *testing.T) {
	// A RoleSystem turn without a Compaction artifact should not
	// trigger the projection.
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

func TestTransform_CompactionAmongOtherArtifacts_OnSameTurn(t *testing.T) {
	// A turn carrying both a Text and a Compaction artifact is
	// recognized as a compaction turn. Both artifacts are preserved
	// in the projection; the Text is the LLM-facing summary and
	// the Compaction is the metadata marker.
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "u0"})
	buf.Append(state.RoleSystem,
		artifact.Text{Content: "summary text"},
		artifact.Compaction{Strategy: "summarize"},
	)

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), buf)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 1)
	require.Len(t, got[0].Artifacts, 2)

	_, isText := got[0].Artifacts[0].(artifact.Text)
	assert.True(t, isText, "first artifact on the turn is the summary Text")

	_, isCompaction := got[0].Artifacts[1].(artifact.Compaction)
	assert.True(t, isCompaction, "second artifact on the turn is the Compaction marker")
}

func TestLatestCompactionIndex(t *testing.T) {
	tests := []struct {
		name string
		in   []state.Turn
		want int
	}{
		{"empty", nil, -1},
		{"no compaction", []state.Turn{textTurnForTransform(state.RoleUser, "u"), textTurnForTransform(state.RoleAssistant, "a")}, -1},
		{"one at start", []state.Turn{compactionTurn(0), textTurnForTransform(state.RoleUser, "u")}, 0},
		{"one at end", []state.Turn{textTurnForTransform(state.RoleUser, "u"), compactionTurn(1)}, 1},
		{"two picks latest", []state.Turn{compactionTurn(0), textTurnForTransform(state.RoleUser, "u"), compactionTurn(2)}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, latestCompactionIndex(tt.in))
		})
	}
}
