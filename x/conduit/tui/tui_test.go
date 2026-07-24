package tui

import (
	"testing"

	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTUI_ImplementsAudioNotifier is a compile-time assertion that
// *TUI satisfies the AudioNotifier contract introduced by the
// framework. The TUI uses the terminal bell (\a) for both done and
// error signals because \a cannot vary pitch; a richer backend could
// introduce distinct tones.
func TestTUI_ImplementsAudioNotifier(t *testing.T) {
	var _ conduit.AudioNotifier = (*TUI)(nil)
}

// TestTUI_DeleteProperty_RemovesKeyFromStatus asserts that the
// model.Update statusMsg handler applies deletion operations by
// removing the key from m.status. Combined with the upstream
// PropertiesEvent handling in tui.go (which converts PropertyOpDelete
// into the deletions slice), this pins the end-to-end delete path.
func TestTUI_DeleteProperty_RemovesKeyFromStatus(t *testing.T) {
	m := newTestModel()
	m.initStatusMsg = nil

	// Seed a key that will later be deleted.
	newM, _ := m.Update(statusMsg{status: map[string]string{"workshop.role": "reviewer"}})
	mm := newM.(*model)
	matched, ok := mm.status["workshop.role"]
	require.True(t, ok)
	require.Equal(t, "reviewer", matched)

	// Apply a delete op: key is removed from m.status.
	newM, _ = mm.Update(statusMsg{deletions: []string{"workshop.role"}})
	mm = newM.(*model)
	_, present := mm.status["workshop.role"]
	assert.False(t, present, "deleted key must be absent from m.status")
}

// TestTUI_StatusMsgApplyOrderSetThenDelete verifies the model applies
// set ops first, then delete ops, so a same-event "set then delete"
// batch yields the correct final state.
func TestTUI_StatusMsgApplyOrderSetThenDelete(t *testing.T) {
	m := newTestModel()
	m.initStatusMsg = nil

	newM, _ := m.Update(statusMsg{status: map[string]string{"k": "v1"}})
	mm := newM.(*model)
	assert.Equal(t, "v1", mm.status["k"])

	// Same-event batch: set k=v2, then delete k. Final state: k absent.
	newM, _ = mm.Update(statusMsg{
		status:    map[string]string{"k": "v2"},
		deletions: []string{"k"},
	})
	mm = newM.(*model)
	_, present := mm.status["k"]
	assert.False(t, present, "delete after set in the same event must remove the key")
}
