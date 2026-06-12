package tui

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/andrewhowdencom/ore/x/conduit/tui/theme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestModel returns a model with a properly initialized textarea widget
// and a dark theme (so tests that reference m.theme.*Style have populated
// values to read). Tests that send key messages or call View() must use
// this helper to avoid panics from the zero-value textarea.
func newTestModel() model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.Focus()
	return model{
		textarea: ta,
		theme:    theme.Dark(),
	}
}

func TestHashToolCallID(t *testing.T) {
	h1 := hashToolCallID("call_abc123")
	h2 := hashToolCallID("call_abc123")
	assert.Equal(t, h1, h2, "hash should be deterministic for the same ID")
	assert.Len(t, h1, 4, "hash should be exactly 4 characters")

	h3 := hashToolCallID("call_different")
	assert.NotEqual(t, h1, h3, "different IDs should produce different hashes")

	// Verify empty string hash is stable and 4 chars.
	hEmpty := hashToolCallID("")
	assert.Len(t, hEmpty, 4, "empty ID hash should be 4 characters")
}

func TestModel_Update_Turn(t *testing.T) {
	m := model{theme: theme.Dark()}
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	// Simulate incremental artifact event arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello world"}})
	mm := newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	require.Len(t, mm2.turns, 1)
	assert.Equal(t, state.RoleAssistant, mm2.turns[0].role)
	require.Len(t, mm2.turns[0].blocks, 1)
	assert.Equal(t, "text", mm2.turns[0].blocks[0].kind)
	assert.Equal(t, "hello world", mm2.turns[0].blocks[0].source)
}

func TestModel_Update_Turn_PreservesReasoning(t *testing.T) {
	m := model{
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		theme:    theme.Dark(),
	}
	// Simulate incremental artifact events arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "the answer is 42"}})
	mm := newM.(*model)
	newM2, _ := mm.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "let me think..."}})
	mm2 := newM2.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM3, _ := mm2.Update(turnMsg{turn: turn})
	mm3 := newM3.(*model)
	require.Len(t, mm3.turns, 1)
	require.Len(t, mm3.turns[0].blocks, 2)
	assert.Equal(t, "text", mm3.turns[0].blocks[0].kind)
	assert.Equal(t, "the answer is 42", mm3.turns[0].blocks[0].source)
	assert.Equal(t, "reasoning", mm3.turns[0].blocks[1].kind)
	assert.Equal(t, "let me think...", mm3.turns[0].blocks[1].source)
	assert.NotEmpty(t, mm3.turns[0].blocks[1].rendered, "reasoning block should be rendered for assistant turns")
}

func TestModel_Update_LifecycleDone_ClearsPending(t *testing.T) {
	m := model{theme: theme.Dark()}
	m.pending = true

	newM, _ := m.Update(lifecycleMsg{phase: "done"})
	mm := newM.(*model)
	assert.False(t, mm.pending)
}

func TestModel_Update_KeyEscape_SendsInterrupt(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	mm := newM.(*model)

	select {
	case e := <-eventsCh:
		require.Equal(t, "interrupt", e.Kind())
	default:
		t.Fatal("expected interrupt event on channel")
	}

	assert.Nil(t, cmd, "Escape should not quit the program")
	_ = mm // suppress unused
}

func TestModel_Update_LifecycleCancelled_ClearsCurrentTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.pending = true
	m.currentTurn = renderedTurn{blocks: []renderedBlock{{kind: "text", source: "partial", title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true}}}

	newM, _ := m.Update(lifecycleMsg{phase: "cancelled"})
	mm := newM.(*model)

	assert.False(t, mm.pending, "pending should be reset")
	assert.Empty(t, mm.currentTurn.blocks, "currentTurn should be cleared")
	assert.Equal(t, "cancelled", mm.status["phase"])
}

func TestModel_Update_Turn_Interleaved(t *testing.T) {
	m := model{theme: theme.Dark()}
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	// Simulate incremental artifact events for interleaved text/reasoning/text.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "Hello"}})
	mm := newM.(*model)
	newM2, _ := mm.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "think"}})
	mm2 := newM2.(*model)
	newM3, _ := mm2.Update(artifactMsg{artifact: artifact.TextDelta{Content: " world"}})
	mm3 := newM3.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM4, _ := mm3.Update(turnMsg{turn: turn})
	mm4 := newM4.(*model)
	require.Len(t, mm4.turns, 1)
	require.Len(t, mm4.turns[0].blocks, 2)
	assert.Equal(t, "text", mm4.turns[0].blocks[0].kind)
	assert.Equal(t, "Hello world", mm4.turns[0].blocks[0].source)
	assert.Equal(t, "reasoning", mm4.turns[0].blocks[1].kind)
	assert.Equal(t, "think", mm4.turns[0].blocks[1].source)
}

func TestModel_Update_Status(t *testing.T) {
	m := model{theme: theme.Dark()}
	newM, _ := m.Update(statusMsg{status: map[string]string{"phase": "thinking..."}})
	mm := newM.(*model)
	assert.Equal(t, "thinking...", mm.status["phase"])
}

func TestModel_Update_KeyRunes(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.KeyPressMsg{Text: "hello"})
	mm := newM.(*model)
	assert.Equal(t, "hello", mm.textarea.Value())
}

func TestModel_Update_KeySpace(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	mm := newM.(*model)
	assert.Equal(t, " ", mm.textarea.Value())
}

func TestModel_Update_Paste(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.PasteMsg{Content: "pasted text"})
	mm := newM.(*model)
	assert.Equal(t, "pasted text", mm.textarea.Value())
}

func TestModel_Update_KeyBackspace(t *testing.T) {
	m := newTestModel()
	m.textarea.SetValue("hi")
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	mm := newM.(*model)
	assert.Equal(t, "h", mm.textarea.Value())
}

func TestModel_Update_KeyBackspace_Empty(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	mm := newM.(*model)
	assert.Empty(t, mm.textarea.Value())
}

func TestModel_Update_KeyEnter_WithInput(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("hello")

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := newM.(*model)

	// User turns no longer render directly on KeyEnter; they arrive via
	// turnMsg from the loop's FanOut.
	assert.Empty(t, mm.turns)
	assert.Empty(t, mm.textarea.Value())

	select {
	case e := <-eventsCh:
		require.Equal(t, "user_message", e.Kind())
		ume, ok := e.(session.UserMessageEvent)
		require.True(t, ok)
		assert.Equal(t, "hello", ume.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestModel_Update_KeyEnter_EmptyInput(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := newM.(*model)

	assert.Empty(t, mm.turns)
	assert.Empty(t, mm.textarea.Value())

	select {
	case <-eventsCh:
		t.Fatal("expected no event on empty input")
	default:
	}
}

func TestModel_Update_WindowSize(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := newM.(*model)
	assert.Equal(t, 80, mm.width)
	assert.Equal(t, 24, mm.height)
}

func TestModel_Update_WindowSize_ResizesViewport(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := newM.(*model)
	assert.Equal(t, 80, mm.viewport.Width())
	assert.Equal(t, 22, mm.viewport.Height())
}

func TestModel_Update_KeyCtrlC(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	mm := newM.(*model)

	select {
	case e := <-eventsCh:
		require.Equal(t, "interrupt", e.Kind())
	default:
		t.Fatal("expected interrupt event on channel")
	}

	require.NotNil(t, cmd)
	_ = mm // suppress unused if we don't assert on mm
}

func TestModel_View_ContainsTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "hello"}}},
	}
	m.syncViewport()
	output := m.View().Content
	assert.Contains(t, output, "You")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "hello")
	idxLabel := strings.Index(output, "You")
	idxContent := strings.Index(output, "hello")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	// The segment from the label to the body must contain exactly one
	// newline — proving the header and body are on consecutive lines
	// (no blank row), but tolerating ANSI codes / padding from lipgloss
	// style emission that may sit between the label and the newline.
	assert.Equal(t, 1, strings.Count(segment, "\n"), "label and body should be separated by exactly one newline")
}

func TestModel_View_ContainsAssistantTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true, kind: "text", source: "world"}}},
	}
	m.syncViewport()
	output := m.View().Content
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "world")
	idxLabel := strings.Index(output, "Assistant")
	idxContent := strings.Index(output, "world")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Equal(t, 1, strings.Count(segment, "\n"), "label and body should be separated by exactly one newline")
}

func TestModel_View_ContainsToolTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleTool, blocks: []renderedBlock{{title: "Tool", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "result"}}},
	}
	m.syncViewport()
	output := m.View().Content
	assert.Contains(t, output, "Tool")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "result")
	idxLabel := strings.Index(output, "Tool")
	idxContent := strings.Index(output, "result")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Equal(t, 1, strings.Count(segment, "\n"), "label and body should be separated by exactly one newline")
}

func TestModel_View_StatusBarFixedBelowInput(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.width = 80
	m.status = map[string]string{"phase": "streaming", "model": "gpt-4o"}
	output := m.View().Content
	// Status should appear after the second separator, below the textarea.
	assert.Contains(t, output, "phase: streaming")
	assert.Contains(t, output, "model: gpt-4o")
	// Verify there are two separators in the output.
	sep := strings.Repeat("─", 80)
	idx1 := strings.Index(output, sep)
	require.GreaterOrEqual(t, idx1, 0, "first separator should exist")
	idx2 := strings.Index(output[idx1+len(sep):], sep)
	require.GreaterOrEqual(t, idx2, 0, "second separator should exist")
}

func TestModel_View_StatusBarHiddenWhenEmpty(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.width = 80
	m.status = map[string]string{"phase": ""}
	output := m.View().Content
	sep := strings.Repeat("─", 80)
	assert.Equal(t, 1, strings.Count(output, sep), "only one separator when status is empty")
	assert.NotContains(t, output, "phase:")
}

func TestModel_View_StatusBarHiddenWhenNil(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.width = 80
	m.status = nil
	output := m.View().Content
	sep := strings.Repeat("─", 80)
	assert.Equal(t, 1, strings.Count(output, sep), "only one separator when status is nil")
}

func TestModel_View_StatusBarWraps(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(20))
	m.width = 40
	// A long status that will wrap at width 40.
	m.status = map[string]string{"phase": "thinking very deeply about the problem at hand"}
	_, statusLines := buildStatusLine(m.theme, m.status, 40)
	require.Greater(t, statusLines, 1, "status should wrap to multiple lines")

	// Verify the wrapped status appears in the output.
	output := m.View().Content
	assert.Contains(t, output, "phase: thinking")

	// Two separators should be present.
	sep := strings.Repeat("─", 40)
	assert.Equal(t, 2, strings.Count(output, sep), "two separators when status is present")
}

func TestModel_RecalcLayout_StatusLines(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(20))
	m.width = 40
	m.height = 24

	// Without status: separatorCount=1, statusLines=0
	m.recalcLayout()
	heightWithoutStatus := m.viewport.Height()

	// With long status that wraps to multiple lines.
	m.status = map[string]string{"phase": "thinking very deeply about the problem at hand"}
	m.recalcLayout()
	heightWithStatus := m.viewport.Height()

	_, statusLines := m.statusLine()
	require.Greater(t, statusLines, 1, "status should wrap to multiple lines")
	// Viewport shrinks by extra separator (1) plus statusLines.
	assert.Equal(t, heightWithoutStatus-1-statusLines, heightWithStatus,
		"viewport should shrink by separatorCount delta and statusLines")
}

func TestModel_View_ContainsPrompt(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.textarea.SetValue("hi")
	output := m.View().Content
	assert.Contains(t, output, "> ")
	assert.Contains(t, output, "hi")
}

func TestModel_View_Empty(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	output := m.View().Content
	assert.Contains(t, output, "> ")
}

func TestModel_View_ContainsInputAtBottom(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: "hello"}}},
	}
	output := m.View().Content
	lines := strings.Split(output, "\n")
	lastLine := lines[len(lines)-1]
	assert.Contains(t, lastLine, "> ")
}

func TestModel_Update_PgUp_ScrollsViewport(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()
	initialYOffset := m.viewport.YOffset()

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	mm := newM.(*model)

	assert.Less(t, mm.viewport.YOffset(), initialYOffset, "PgUp should scroll viewport up")
}

func TestModel_Update_PgDown_ScrollsViewport(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()

	// Scroll up first so PgDown has room to scroll back down
	m.viewport.HalfPageUp()
	initialYOffset := m.viewport.YOffset()

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	mm := newM.(*model)

	assert.Greater(t, mm.viewport.YOffset(), initialYOffset, "PgDown should scroll viewport down")
}

func TestModel_Update_Turn_AutoScrollsViewport(t *testing.T) {
	t.Run("at bottom", func(t *testing.T) {
		m := newTestModel()
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
		// Pre-populate with tall content so buildContent() exceeds viewport height.
		m.turns = []renderedTurn{
			{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: strings.Repeat("word ", 200)}}},
		}
		m.viewport.SetContent(m.buildContent())
		m.viewport.GotoBottom()
		oldBottom := m.viewport.YOffset()
		require.True(t, m.viewport.AtBottom(), "should start at bottom")

		// Add another tall turn to genuinely increase content height.
		turn := state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: strings.Repeat("more content ", 200)},
			},
		}
		newM, _ := m.Update(turnMsg{turn: turn})
		mm := newM.(*model)

		assert.True(t, mm.viewport.AtBottom(), "turn should auto-scroll viewport to bottom")
		assert.Greater(t, mm.viewport.YOffset(), oldBottom, "should scroll to new bottom past old bottom")
	})

	t.Run("scrolled up", func(t *testing.T) {
		m := newTestModel()
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
		m.turns = []renderedTurn{
			{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: strings.Repeat("word ", 200)}}},
		}
		m.viewport.SetContent(m.buildContent())
		m.viewport.GotoBottom()
		m.viewport.HalfPageUp()
		oldBottom := m.viewport.YOffset()
		require.False(t, m.viewport.AtBottom(), "should not be at bottom after scrolling up")

		turn := state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: strings.Repeat("more content ", 200)},
			},
		}
		newM, _ := m.Update(turnMsg{turn: turn})
		mm := newM.(*model)

		assert.False(t, mm.viewport.AtBottom(), "turn should not auto-scroll viewport when user has scrolled up")
		assert.Equal(t, oldBottom, mm.viewport.YOffset(), "viewport should stay at user's scroll position")
	})
}

func TestModel_View_LongHistory_InputAtBottom(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	// Add enough turns to exceed viewport height
	for i := 0; i < 10; i++ {
		m.turns = append(m.turns, renderedTurn{
			role:   state.RoleUser,
			blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: strings.Repeat("word ", 20)}},
		})
	}
	output := m.View().Content
	lines := strings.Split(output, "\n")
	lastLine := lines[len(lines)-1]
	assert.Contains(t, lastLine, "> ")
}

func TestModel_View_WrapsLongTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(20), viewport.WithHeight(5))
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: strings.Repeat("word ", 10)}}},
	}
	m.syncViewport()
	output := m.View().Content
	lines := strings.Split(output, "\n")
	// Find the label line
	labelIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "You") {
			labelIdx = i
			break
		}
	}
	require.GreaterOrEqual(t, labelIdx, 0, "should contain label line")
	// Count content lines (before separator)
	contentLines := 0
	for i := labelIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "─") {
			break
		}
		if strings.TrimSpace(lines[i]) != "" {
			contentLines++
		}
	}
	assert.Greater(t, contentLines, 1, "long content should wrap to multiple lines at column 0")
	// Verify no old indent prefix exists (skip viewport padding lines)
	for i := labelIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "─") {
			break
		}
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		assert.False(t, strings.HasPrefix(lines[i], "     "), "content should not have old indent prefix")
	}
}

// unknownArtifact is an artifact type not handled by the TUI model.
type unknownArtifact struct{}

func (unknownArtifact) Kind() string { return "unknown" }

func TestModel_Update_Turn_Assistant_PopulatesRendered(t *testing.T) {
	m := model{
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		theme:    theme.Dark(),
	}
	// Simulate incremental artifact event with Markdown text.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "# Hello\n\n**bold** text"}})
	mm := newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	require.Len(t, mm2.turns, 1)
	require.Len(t, mm2.turns[0].blocks, 1)
	assert.NotEmpty(t, mm2.turns[0].blocks[0].source)
	assert.NotEmpty(t, mm2.turns[0].blocks[0].rendered, "assistant turn should have rendered Markdown")
}

func TestModel_Update_Turn_User_RendersMarkdown(t *testing.T) {
	m := model{
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		theme:    theme.Dark(),
		md:       mockMarkdownRenderer{output: "rendered hello world"},
	}
	turn := state.Turn{
		Role: state.RoleUser,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello world"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Equal(t, "rendered hello world", mm.turns[0].blocks[0].rendered, "user turn should be markdown rendered")
}

func TestModel_Update_WindowSize_RerendersAssistantTurns(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	// Simulate incremental artifact event with text that wraps differently at different widths.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "# Title\n\nThis is a longer paragraph that should definitely wrap differently at width forty versus width eighty."}})
	mm := newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	require.Len(t, mm2.turns, 1)
	initialRendered := mm2.turns[0].blocks[0].rendered
	assert.NotEmpty(t, initialRendered)

	// Resize to a narrower width
	newM3, _ := mm2.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	mm3 := newM3.(*model)
	assert.NotEmpty(t, mm3.turns[0].blocks[0].rendered)
	assert.NotEqual(t, initialRendered, mm3.turns[0].blocks[0].rendered,
		"re-rendered output should differ after width change")
}

// mockMarkdownRenderer is a test double that returns fixed output or errors.
type mockMarkdownRenderer struct {
	output string
	err    error
}

func (m mockMarkdownRenderer) Render(text string, width int) (string, error) {
	return m.output, m.err
}

func TestModel_Update_Turn_Assistant_RenderError_Fallback(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{err: errors.New("render failed")}
	// Simulate incremental artifact event; render error should leave rendered empty.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "# Hello"}})
	mm := newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	require.Len(t, mm2.turns, 1)
	require.Len(t, mm2.turns[0].blocks, 1)
	assert.Empty(t, mm2.turns[0].blocks[0].rendered, "render error should leave rendered empty")
	assert.Equal(t, "# Hello", mm2.turns[0].blocks[0].source, "raw text should still be stored")
}

func TestModel_View_AssistantTurn_RenderError_FallbackToPlainText(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{err: errors.New("render failed")}
	// Simulate incremental artifact event.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "plain fallback text"}})
	mm := newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	output := mm2.View().Content
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "plain fallback text")
}

func TestModel_Update_WindowSize_RerenderError_KeepsOldCache(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "initial-render"}
	// Simulate incremental artifact event.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "text"}})
	mm := newM.(*model)
	newM, _ = mm.Update(renderTickMsg{})
	mm = newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	assert.Equal(t, "initial-render", mm2.turns[0].blocks[0].rendered)

	// Swap to an error-returning renderer and resize.
	mm2.md = mockMarkdownRenderer{err: errors.New("resize render failed")}
	newM3, _ := mm2.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	mm3 := newM3.(*model)
	assert.Equal(t, "initial-render", mm3.turns[0].blocks[0].rendered,
		"old cache should be kept on re-render error")
}

func TestModel_Update_ShiftEnter_InsertsNewline(t *testing.T) {
	m := newTestModel()
	m.textarea.SetValue("hello")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	mm := newM.(*model)

	assert.Contains(t, mm.textarea.Value(), "\n")
}

func TestModel_Update_Enter_SubmitsMultiLine(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("line1\nline2")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := newM.(*model)

	assert.Empty(t, mm.textarea.Value())

	select {
	case e := <-eventsCh:
		require.Equal(t, "user_message", e.Kind())
		ume, ok := e.(session.UserMessageEvent)
		require.True(t, ok)
		assert.Equal(t, "line1\nline2", ume.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestModel_View_ContainsSeparator(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.width = 80
	m.syncViewport()
	output := m.View().Content
	assert.Contains(t, output, strings.Repeat("─", 80))
}

func TestModel_Update_Turn_Assistant_EmptyText(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "mock-empty-output"}
	// Simulate incremental artifact event with empty text.
	newM, _ := m.Update(artifactMsg{artifact: artifact.Text{Content: ""}})
	mm := newM.(*model)
	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	require.Len(t, mm2.turns, 1)
	require.Len(t, mm2.turns[0].blocks, 1)
	assert.Empty(t, mm2.turns[0].blocks[0].source)
	assert.Equal(t, "mock-empty-output", mm2.turns[0].blocks[0].rendered)
	// View should not crash with empty text.
	output := mm2.View().Content
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
}

// --- Critical coverage gap tests (added per testing agent review) ---

func TestModel_Update_ShiftEnter_DoesNotEmitEvent(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("hello")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	mm := newM.(*model)

	assert.Contains(t, mm.textarea.Value(), "\n")

	select {
	case <-eventsCh:
		t.Fatal("Shift+Enter should not emit a UserMessageEvent")
	default:
	}
}

func TestModel_Update_CtrlJ_InsertsNewline(t *testing.T) {
	m := newTestModel()
	m.textarea.SetValue("hello")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	mm := newM.(*model)

	assert.Contains(t, mm.textarea.Value(), "\n")
}

func TestModel_Update_CtrlJ_DoesNotEmitEvent(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("hello")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	mm := newM.(*model)

	assert.Contains(t, mm.textarea.Value(), "\n")

	select {
	case <-eventsCh:
		t.Fatal("Ctrl+J should not emit a UserMessageEvent")
	default:
	}
}

func TestModel_Update_CtrlJ_EmptyTextarea(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, _ := m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	mm := newM.(*model)

	assert.Equal(t, "\n", mm.textarea.Value())

	select {
	case <-eventsCh:
		t.Fatal("Ctrl+J on empty textarea should not emit event")
	default:
	}
}

func TestModel_Update_DynamicLayout(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := newM.(*model)

	// Empty textarea: 1 line, separator: 1 line, viewport: 22 lines
	assert.Equal(t, 1, mm.textarea.Height(), "empty textarea should be 1 line")
	assert.Equal(t, 22, mm.viewport.Height(), "viewport should fill remaining space")

	// Add 3 lines
	mm.textarea.SetValue("line1\nline2\nline3")
	mm.recalcLayout()

	assert.Equal(t, 3, mm.textarea.Height(), "textarea should grow to 3 lines")
	assert.Equal(t, 20, mm.viewport.Height(), "viewport should shrink accordingly")

	// Add many lines to hit the cap: max(3, 24/3) = 8
	mm.textarea.SetValue(strings.Repeat("x\n", 20))
	mm.recalcLayout()

	assert.Equal(t, 8, mm.textarea.Height(), "should respect max height cap")
	assert.Equal(t, 15, mm.viewport.Height(), "viewport should shrink to minimum")
}

func TestModel_View_SeparatorAdaptsToResize(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.width = 80
	m.status = map[string]string{"phase": "ready"} // ensure viewport has content so separator is rendered
	m.syncViewport()
	output := m.View().Content
	assert.Contains(t, output, strings.Repeat("─", 80))

	// Resize to narrower width
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	mm := newM.(*model)
	mm.status = map[string]string{"phase": "ready"}
	output = mm.View().Content
	assert.Contains(t, output, strings.Repeat("─", 50))
}

func TestModel_Update_ShiftEnter_EmptyTextarea(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	mm := newM.(*model)

	assert.Equal(t, "\n", mm.textarea.Value())

	select {
	case <-eventsCh:
		t.Fatal("Shift+Enter on empty textarea should not emit event")
	default:
	}
}

func TestModel_Update_RecalcLayout_MinimumViewportHeight(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 2})
	mm := newM.(*model)

	// Even with a tiny terminal, viewport should never collapse to 0
	assert.GreaterOrEqual(t, mm.viewport.Height(), 1, "viewport height should never be < 1")
}

func TestModel_WindowTitle_Submitted(t *testing.T) {
	m := newTestModel()
	m.name = "Ore"
	m.status = map[string]string{"phase": "submitted"}
	assert.Equal(t, "Ore [...]", m.windowTitle())
}

func TestModel_WindowTitle_Streaming(t *testing.T) {
	m := newTestModel()
	m.name = "Ore"
	m.status = map[string]string{"phase": "streaming"}
	assert.Equal(t, "Ore [...]", m.windowTitle())
}

func TestModel_WindowTitle_Done(t *testing.T) {
	m := newTestModel()
	m.name = "Ore"
	m.status = map[string]string{"phase": "done"}
	assert.Equal(t, "Ore [ok]", m.windowTitle())
}

func TestModel_WindowTitle_Error(t *testing.T) {
	m := newTestModel()
	m.name = "Ore"
	m.status = map[string]string{"phase": "error"}
	assert.Equal(t, "Ore [err]", m.windowTitle())
}

func TestModel_WindowTitle_Initial(t *testing.T) {
	m := newTestModel()
	m.name = "Ore"
	assert.Equal(t, "Ore [ok]", m.windowTitle())
}

func TestModel_WindowTitle_CustomName(t *testing.T) {
	m := newTestModel()
	m.name = "tui-chat"
	m.status = map[string]string{"phase": "streaming"}
	assert.Equal(t, "tui-chat [...]", m.windowTitle())
}

func TestModel_WindowTitle_Cancelled(t *testing.T) {
	m := newTestModel()
	m.name = "Ore"
	m.status = map[string]string{"phase": "cancelled"}
	assert.Equal(t, "Ore [cancelled]", m.windowTitle())
}

func TestModel_Update_LifecycleSubmittedThenDone(t *testing.T) {
	m := model{theme: theme.Dark()}

	newM, _ := m.Update(lifecycleMsg{phase: "submitted"})
	mm := newM.(*model)
	assert.True(t, mm.pending, "submitted should set pending")

	newM2, _ := mm.Update(lifecycleMsg{phase: "done"})
	mm2 := newM2.(*model)
	assert.False(t, mm2.pending, "done should clear pending")
}

func TestModel_Update_Turn_User_DoesNotClearPending(t *testing.T) {
	m := model{theme: theme.Dark()}
	m.pending = true

	turn := state.Turn{
		Role: state.RoleUser,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "user message"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	assert.True(t, mm.pending, "user turn should not clear pending")
}

func TestModel_Update_ClearPendingMsg(t *testing.T) {
	m := model{theme: theme.Dark()}
	m.pending = true

	newM, _ := m.Update(clearPendingMsg{})
	mm := newM.(*model)

	assert.False(t, mm.pending, "clearPendingMsg should reset pending")
}

func TestModel_Update_RapidSubmissions(t *testing.T) {
	eventsCh := make(chan session.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("first")

	// First submission
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := newM.(*model)
	assert.Empty(t, mm.textarea.Value())

	// Second submission
	mm.textarea.SetValue("second")
	newM2, _ := mm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm2 := newM2.(*model)
	assert.Empty(t, mm2.textarea.Value())

	// Both events should be on the channel
	select {
	case e := <-eventsCh:
		ume, ok := e.(session.UserMessageEvent)
		require.True(t, ok)
		assert.Equal(t, "first", ume.Content)
	default:
		t.Fatal("expected first event on channel")
	}
	select {
	case e := <-eventsCh:
		ume, ok := e.(session.UserMessageEvent)
		require.True(t, ok)
		assert.Equal(t, "second", ume.Content)
	default:
		t.Fatal("expected second event on channel")
	}
}

func TestAutoScroll_MultipleTurns(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	for i := 0; i < 3; i++ {
		m.viewport.GotoBottom() // Ensure viewport starts at bottom for each iteration
		// Simulate incremental artifact event for each assistant turn.
		newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: strings.Repeat("content ", 200)}})
		m = *newM.(*model)
		turn := state.Turn{
			Role: state.RoleAssistant,
		}
		newM2, _ := m.Update(turnMsg{turn: turn})
		m = *newM2.(*model)
		assert.True(t, m.viewport.AtBottom(), "turn %d should auto-scroll viewport to bottom", i+1)
	}
}

func TestModel_Update_TurnMsg_DoesNotScrollWhenNotAtBottom(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: strings.Repeat("word ", 200)}}},
	}
	m.viewport.SetContent(m.buildContent())
	m.viewport.GotoBottom()
	m.viewport.HalfPageUp()
	oldYOffset := m.viewport.YOffset()
	require.False(t, m.viewport.AtBottom(), "should not be at bottom after scrolling up")

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: strings.Repeat("more content ", 200)},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)

	assert.False(t, mm.viewport.AtBottom(), "turnMsg should not scroll when not at bottom")
	assert.Equal(t, oldYOffset, mm.viewport.YOffset(), "viewport YOffset should not change")
}

func TestModel_Update_RenderTickMsg_DoesNotScrollWhenNotAtBottom(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: strings.Repeat("word ", 200)}}},
	}
	m.viewport.SetContent(m.buildContent())
	m.viewport.GotoBottom()
	require.True(t, m.viewport.AtBottom(), "should start at bottom")

	// Send a text delta that schedules a render tick.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)

	// Scroll up before the tick fires.
	mm.viewport.HalfPageUp()
	oldYOffset := mm.viewport.YOffset()
	require.False(t, mm.viewport.AtBottom(), "should not be at bottom after scrolling up")

	// Fire the render tick.
	newM2, _ := mm.Update(renderTickMsg{})
	mm2 := newM2.(*model)

	assert.False(t, mm2.viewport.AtBottom(), "renderTickMsg should not scroll when not at bottom")
	assert.Equal(t, oldYOffset, mm2.viewport.YOffset(), "viewport YOffset should not change")
}

func TestUnknownArtifact_Ignored(t *testing.T) {
	m := model{theme: theme.Dark()}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			unknownArtifact{},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	assert.Empty(t, mm.turns[0].blocks, "unknown artifact should produce no blocks")
}

func TestModel_Update_KeyCtrlO_TogglesExpandLatestDetails(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	assert.False(t, m.expandLatestDetails)

	newM, _ := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm := newM.(*model)
	assert.True(t, mm.expandLatestDetails)

	newM2, _ := mm.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm2 := newM2.(*model)
	assert.False(t, mm2.expandLatestDetails)
}

func TestModel_Update_Turn_Assistant_ResetsExpandLatestDetails(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.expandLatestDetails = true

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	assert.False(t, mm.expandLatestDetails)
}

func TestModel_Update_UserAfterTool_DoesNotResetExpand(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	// Simulate an assistant turn with a tool call
	m.turns = append(m.turns, renderedTurn{
		role:   state.RoleAssistant,
		blocks: []renderedBlock{{title: "Tool", style: m.theme.AssistantStyle, expandedByDefault: false, kind: "tool_call", source: "Calling: foo({})", compact: "foo", toolCallID: "call_1"}},
	})
	// Simulate a tool result turn
	m.turns = append(m.turns, renderedTurn{
		role:   state.RoleTool,
		blocks: []renderedBlock{{title: "Tool Result", style: m.theme.ToolResultStyle, expandedByDefault: false, kind: "tool_result", source: "result", compact: "result", toolCallID: "call_1"}},
	})
	m.expandLatestDetails = true

	// User turn should NOT reset expandLatestDetails
	turn := state.Turn{
		Role: state.RoleUser,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)

	assert.True(t, mm.expandLatestDetails, "user turn should not reset expandLatestDetails")

	// Previous assistant turn's tool blocks remain expanded
	output := mm.buildContent()
	assert.Contains(t, output, "Calling: foo({})")
}

func TestModel_Update_KeyCtrlO_WhilePending(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.pending = true

	// Toggle should not panic while a response is pending
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm := newM.(*model)
	assert.True(t, mm.expandLatestDetails)
	assert.True(t, mm.pending)

	// View should still show the pending placeholder
	output := mm.View().Content
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "...")
}

func TestModel_Update_KeyCtrlO_RapidToggles(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	newM, _ := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm := newM.(*model)
	assert.True(t, mm.expandLatestDetails)

	newM2, _ := mm.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm2 := newM2.(*model)
	assert.False(t, mm2.expandLatestDetails)

	newM3, _ := mm2.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm3 := newM3.(*model)
	assert.True(t, mm3.expandLatestDetails)
}

func TestModel_Update_AudioMsg(t *testing.T) {
	m := newTestModel()
	m.pending = true
	m.status = map[string]string{"phase": "thinking..."}

	// Capture stdout to verify the terminal bell is printed.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	newM, cmd := m.Update(audioMsg{})
	mm := newM.(*model)

	w.Close()
	os.Stdout = oldStdout

	out, err := io.ReadAll(r)
	require.NoError(t, err)

	// audioMsg is a pure side-effect; model state should be unchanged.
	assert.True(t, mm.pending, "audioMsg should not alter pending")
	assert.Equal(t, "thinking...", mm.status["phase"], "audioMsg should not alter status")
	assert.Nil(t, cmd)
	assert.Equal(t, "\a", string(out), "audioMsg should print BEL to stdout")
}

func TestModel_Update_ErrorMsg(t *testing.T) {
	m := newTestModel()
	m.pending = true

	newM, cmd := m.Update(errorMsg{err: errors.New("boom")})
	mm := newM.(*model)

	assert.False(t, mm.pending, "errorMsg should clear pending")
	require.Len(t, mm.turns, 1, "errorMsg should append a system error turn")
	assert.Equal(t, state.RoleSystem, mm.turns[0].role)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Equal(t, "error", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "boom", mm.turns[0].blocks[0].source)
	assert.Nil(t, cmd)
}

func TestModel_Update_ErrorMsg_Empty(t *testing.T) {
	m := newTestModel()
	m.pending = true

	newM, cmd := m.Update(errorMsg{err: errors.New("")})
	mm := newM.(*model)

	assert.False(t, mm.pending, "errorMsg should clear pending")
	require.Len(t, mm.turns, 1, "errorMsg should append a system error turn")
	assert.Equal(t, state.RoleSystem, mm.turns[0].role)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Equal(t, "error", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "", mm.turns[0].blocks[0].source)
	assert.Nil(t, cmd)
}

func TestModel_Update_Status_Merges(t *testing.T) {
	m := model{theme: theme.Dark()}
	newM, _ := m.Update(statusMsg{status: map[string]string{"thread_id": "abc", "phase": "ready"}})
	mm := newM.(*model)
	assert.Equal(t, "abc", mm.status["thread_id"])
	assert.Equal(t, "ready", mm.status["phase"])

	// Second update should merge, overwriting present keys and leaving absent ones.
	newM2, _ := mm.Update(statusMsg{status: map[string]string{"phase": "thinking..."}})
	mm2 := newM2.(*model)
	assert.Equal(t, "abc", mm2.status["thread_id"], "thread_id should be preserved")
	assert.Equal(t, "thinking...", mm2.status["phase"], "phase should be overwritten")
}

func TestModel_Update_Status_LabelRewriting(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.statusLabels = map[string]string{"workshop.role": "role"}

	newM, _ := m.Update(statusMsg{status: map[string]string{"workshop.role": "reviewer"}})
	mm := newM.(*model)

	str, _ := mm.statusLine()
	assert.Contains(t, str, "role: reviewer", "label should be rewritten to display label")
	assert.NotContains(t, str, "workshop.role: reviewer", "raw key should not appear in output")
}

func TestModel_Update_Status_ZoneFormatter_LabelRewriting(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.zoneFormatter = func(status map[string]string) []conduit.StatusSegment {
		var segments []conduit.StatusSegment
		for k, v := range status {
			segments = append(segments, conduit.StatusSegment{
				Label: k,
				Value: v,
				Zone:  "context",
			})
		}
		return segments
	}
	m.zonePriorities = map[string]int{"context": 1}
	m.statusLabels = map[string]string{"workshop.role": "role"}

	newM, _ := m.Update(statusMsg{status: map[string]string{
		"workshop.role": "reviewer",
		"phase":         "done",
	}})
	mm := newM.(*model)

	str, _ := mm.statusLine()
	assert.Contains(t, str, "role: reviewer", "label should be rewritten in zone context")
	assert.NotContains(t, str, "workshop.role: reviewer", "raw key should not appear in output")
	assert.Contains(t, str, "phase: done", "unmapped key should render unchanged")
}

func TestModel_Update_Status_ZoneFormatter(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.zoneFormatter = func(status map[string]string) []conduit.StatusSegment {
		var segments []conduit.StatusSegment
		for k, v := range status {
			zone := "default"
			if k == "phase" || k == "title" {
				zone = "lifecycle"
			} else if k == "thread_id" || k == "model" {
				zone = "context"
			}
			segments = append(segments, conduit.StatusSegment{
				Label: k,
				Value: v,
				Zone:  zone,
			})
		}
		return segments
	}
	m.zonePriorities = map[string]int{
		"lifecycle": 0,
		"context":   1,
		"default":   99,
	}

	newM, _ := m.Update(statusMsg{status: map[string]string{
		"phase":     "streaming",
		"thread_id": "abc-123",
	}})
	mm := newM.(*model)

	str, _ := mm.statusLine()
	assert.Contains(t, str, "Lifecycle:")
	assert.Contains(t, str, "phase: streaming")
	assert.Contains(t, str, "Context:")
	assert.Contains(t, str, "thread_id: abc-123")
}

// --- Incremental artifact rendering tests (issue #217) ---

func TestModel_Update_ArtifactMsg_AccumulatesBlocks(t *testing.T) {
	m := model{theme: theme.Dark()}
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	// First artifact: text block
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)
	require.Len(t, mm.currentTurn.blocks, 1)
	assert.Equal(t, "text", mm.currentTurn.blocks[0].kind)
	assert.Equal(t, "hello", mm.currentTurn.blocks[0].source)

	// Second artifact: reasoning block
	newM2, _ := mm.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "thinking..."}})
	mm2 := newM2.(*model)
	require.Len(t, mm2.currentTurn.blocks, 2)
	assert.Equal(t, "reasoning", mm2.currentTurn.blocks[1].kind)
}

func TestModel_Update_ArtifactMsg_FinalizedByTurnMsg(t *testing.T) {
	m := model{theme: theme.Dark()}
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "response"}})
	mm := newM.(*model)
	require.Len(t, mm.currentTurn.blocks, 1)

	turn := state.Turn{
		Role: state.RoleAssistant,
	}
	newM2, _ := mm.Update(turnMsg{turn: turn})
	mm2 := newM2.(*model)
	require.Len(t, mm2.turns, 1)
	require.Len(t, mm2.turns[0].blocks, 1)
	assert.Len(t, mm2.currentTurn.blocks, 0, "currentTurn should be cleared after finalization")
	assert.Equal(t, "response", mm2.turns[0].blocks[0].source)
}

func TestModel_Update_ArtifactMsg_ClearedByError(t *testing.T) {
	m := model{theme: theme.Dark()}
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.pending = true
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "partial"}})
	mm := newM.(*model)
	require.Len(t, mm.currentTurn.blocks, 1)

	newM2, _ := mm.Update(errorMsg{err: errors.New("failed")})
	mm2 := newM2.(*model)
	assert.Len(t, mm2.currentTurn.blocks, 0, "currentTurn should be cleared on error")
	assert.False(t, mm2.pending)
	require.Len(t, mm2.turns, 1, "errorMsg should append a system error turn")
	assert.Equal(t, state.RoleSystem, mm2.turns[0].role)
	assert.Equal(t, "error", mm2.turns[0].blocks[0].kind)
	assert.Equal(t, "failed", mm2.turns[0].blocks[0].source)
}

func TestModel_View_ContainsCurrentTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.width = 80
	m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "in-progress"}})
	m.Update(renderTickMsg{})
	output := m.View().Content
	assert.Contains(t, output, "Assistant")
	assert.NotContains(t, output, "· |s|")
	assert.Contains(t, output, "in-progress")
}

func TestModel_View_PendingWithCurrentTurn_HidesPlaceholder(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.width = 80
	m.pending = true
	// Before any artifact arrives, the placeholder should be visible
	m.syncViewport()
	output1 := m.View().Content
	assert.Contains(t, output1, "...")

	// After artifact arrives, placeholder should be replaced by actual content
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "real content"}})
	mm := newM.(*model)
	mm.syncViewport()
	output2 := mm.View().Content
	assert.Contains(t, output2, "real")
	assert.NotContains(t, output2, "...", "placeholder should be hidden when currentTurn has blocks")
}

// recordingMockRenderer captures the width passed to each Render call.
type recordingMockRenderer struct {
	widths []int
	output string
}

func (r *recordingMockRenderer) Render(text string, width int) (string, error) {
	r.widths = append(r.widths, width)
	return r.output, nil
}

func TestModel_Update_MixedArtifacts_AccumulateInOrder(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "rendered"}

	// Simulate incremental artifact events arriving before TurnCompleteEvent.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)
	newM2, _ := mm.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: "think"}})
	mm2 := newM2.(*model)
	newM3, _ := mm2.Update(artifactMsg{artifact: artifact.ToolCall{ID: "call_1", Name: "foo", Arguments: "{}"}})
	mm3 := newM3.(*model)

	require.Len(t, mm3.currentTurn.blocks, 3)
	assert.Equal(t, "text", mm3.currentTurn.blocks[0].kind)
	assert.Equal(t, "hello", mm3.currentTurn.blocks[0].source)
	assert.Equal(t, "reasoning", mm3.currentTurn.blocks[1].kind)
	assert.Equal(t, "think", mm3.currentTurn.blocks[1].source)
	assert.Equal(t, "tool_call", mm3.currentTurn.blocks[2].kind)
	assert.Equal(t, "call_1", mm3.currentTurn.blocks[2].toolCallID)

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello"},
			artifact.Reasoning{Content: "think"},
			artifact.ToolCall{ID: "call_1", Name: "foo", Arguments: "{}"},
		},
	}
	newM4, _ := mm3.Update(turnMsg{turn: turn})
	mm4 := newM4.(*model)
	require.Len(t, mm4.turns, 1)
	require.Len(t, mm4.turns[0].blocks, 3)
	assert.Equal(t, "text", mm4.turns[0].blocks[0].kind)
	assert.Equal(t, "reasoning", mm4.turns[0].blocks[1].kind)
	assert.Equal(t, "call_1", mm4.turns[0].blocks[2].toolCallID)
	assert.Equal(t, "tool_call", mm4.turns[0].blocks[2].kind)
}

func TestModel_Update_UnknownArtifactType_Ignored(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	// Sending an unhandled artifact type should not panic or add blocks.
	newM, _ := m.Update(artifactMsg{artifact: unknownArtifact{}})
	mm := newM.(*model)
	assert.Len(t, mm.currentTurn.blocks, 0)
	assert.Len(t, mm.turns, 0)
}

func TestModel_Update_ErrorMidStream_ClearsAndRecovers(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "rendered"}
	m.pending = true // Simulate an in-flight assistant turn.

	// Start streaming an assistant turn.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "partial"}})
	mm := newM.(*model)
	require.Len(t, mm.currentTurn.blocks, 1)
	assert.True(t, mm.pending)

	// Error arrives mid-stream: currentTurn should be cleared and pending reset.
	newM2, _ := mm.Update(errorMsg{err: errors.New("network error")})
	mm2 := newM2.(*model)
	assert.Len(t, mm2.currentTurn.blocks, 0)
	assert.False(t, mm2.pending)

	// New turn starts after error (artifactMsg accumulates blocks; pending
	// remains false until a new user submission sets it).
	newM3, _ := mm2.Update(artifactMsg{artifact: artifact.TextDelta{Content: "new content"}})
	mm3 := newM3.(*model)
	require.Len(t, mm3.currentTurn.blocks, 1)
	assert.Equal(t, "new content", mm3.currentTurn.blocks[0].source)
	assert.False(t, mm3.pending)

	// Finalize the recovery turn.
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "new content"},
		},
	}
	newM4, _ := mm3.Update(turnMsg{turn: turn})
	mm4 := newM4.(*model)
	require.Len(t, mm4.turns, 2)
	assert.Equal(t, state.RoleSystem, mm4.turns[0].role)
	assert.Equal(t, "error", mm4.turns[0].blocks[0].kind)
	assert.Equal(t, "network error", mm4.turns[0].blocks[0].source)
	assert.Equal(t, state.RoleAssistant, mm4.turns[1].role)
	assert.Equal(t, "new content", mm4.turns[1].blocks[0].source)
	assert.False(t, mm4.pending)
}

func TestModel_Update_WindowSize_RerendersCurrentTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	rec := &recordingMockRenderer{output: "rendered"}
	m.md = rec

	// Send artifact at initial viewport width (80).
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)
	newM, _ = mm.Update(renderTickMsg{})
	mm = newM.(*model)
	require.Len(t, mm.currentTurn.blocks, 1)
	assert.Equal(t, "rendered", mm.currentTurn.blocks[0].rendered)
	require.Len(t, rec.widths, 1)
	assert.Equal(t, 80, rec.widths[0]) // full viewport width

	// Resize viewport to narrower width.
	newM2, _ := mm.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	mm2 := newM2.(*model)

	// Verify the currentTurn block was re-rendered with the new width.
	require.Len(t, rec.widths, 2)
	assert.Equal(t, 40, rec.widths[1]) // new full viewport width
	assert.Equal(t, "rendered", mm2.currentTurn.blocks[0].rendered)
}

func TestModel_Update_DeltaAccumulationAndDebounce(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.md = mockMarkdownRenderer{output: "rendered"}

	// Send two TextDelta messages rapidly — they should accumulate
	// into a single text block, with a render tick pending.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)
	newM2, _ := mm.Update(artifactMsg{artifact: artifact.TextDelta{Content: " world"}})
	mm2 := newM2.(*model)

	// Before the tick fires, both deltas should be merged into one block.
	require.Len(t, mm2.currentTurn.blocks, 1)
	assert.Equal(t, "text", mm2.currentTurn.blocks[0].kind)
	assert.Equal(t, "hello world", mm2.currentTurn.blocks[0].source)
	assert.Empty(t, mm2.currentTurn.blocks[0].rendered, "rendered should be empty before tick")
	assert.True(t, mm2.renderScheduled, "render tick should be scheduled")

	// Simulate the render tick firing.
	newM3, _ := mm2.Update(renderTickMsg{})
	mm3 := newM3.(*model)

	assert.Equal(t, "rendered", mm3.currentTurn.blocks[0].rendered, "tick should populate rendered")
	assert.False(t, mm3.renderScheduled, "renderScheduled should be cleared after tick")
}

func TestModel_Update_EmptyDelta_Ignored(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	// Empty TextDelta should not create a block.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: ""}})
	mm := newM.(*model)
	assert.Empty(t, mm.currentTurn.blocks, "empty TextDelta should not add a block")

	// Empty ReasoningDelta should not create a block.
	newM2, _ := mm.Update(artifactMsg{artifact: artifact.ReasoningDelta{Content: ""}})
	mm2 := newM2.(*model)
	assert.Empty(t, mm2.currentTurn.blocks, "empty ReasoningDelta should not add a block")

	// No tick should be scheduled.
	assert.False(t, mm2.renderScheduled, "empty delta should not schedule a tick")
}

func TestModel_Update_ErrorCancelsPendingTick(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	// Send a delta to schedule a tick.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)
	assert.True(t, mm.renderScheduled, "tick should be scheduled")

	// Error arrives — should cancel the pending tick.
	newM2, _ := mm.Update(errorMsg{err: errors.New("boom")})
	mm2 := newM2.(*model)
	assert.False(t, mm2.renderScheduled, "error should cancel pending tick")

	// Simulate the tick firing after error — should be ignored.
	newM3, _ := mm2.Update(renderTickMsg{})
	mm3 := newM3.(*model)
	assert.False(t, mm3.renderScheduled, "tick should remain false after ignored tick")
	assert.Empty(t, mm3.currentTurn.blocks, "currentTurn should stay empty after error")
}

func TestModel_Update_WindowResizeCancelsPendingTick(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	rec := &recordingMockRenderer{output: "rendered"}
	m.md = rec

	// Send a delta at width 80 to schedule a tick.
	newM, _ := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello"}})
	mm := newM.(*model)
	assert.True(t, mm.renderScheduled, "tick should be scheduled")
	assert.Len(t, rec.widths, 0, "delta should not trigger immediate render")

	// Resize to width 40 before the tick fires.
	newM2, _ := mm.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	mm2 := newM2.(*model)
	assert.False(t, mm2.renderScheduled, "resize should cancel pending tick")
	assert.Equal(t, 40, rec.widths[0], "resize should re-render currentTurn at new width")

	// Simulate the tick firing after resize — should be ignored.
	newM3, _ := mm2.Update(renderTickMsg{})
	mm3 := newM3.(*model)
	assert.False(t, mm3.renderScheduled, "tick should remain false after ignored tick")
	assert.Len(t, rec.widths, 1, "tick after resize should not trigger extra render")
}

func TestModel_Update_MultipleDeltas_SingleTick(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	// First delta should schedule a tick.
	newM, cmd1 := m.Update(artifactMsg{artifact: artifact.TextDelta{Content: "hello"}})
	mm1 := newM.(*model)
	assert.NotNil(t, cmd1, "first delta should return a tick command")
	assert.True(t, mm1.renderScheduled, "renderScheduled should be true after first delta")

	// Second delta while tick is pending should NOT schedule another tick.
	newM2, cmd2 := mm1.Update(artifactMsg{artifact: artifact.TextDelta{Content: " world"}})
	mm2 := newM2.(*model)
	assert.Nil(t, cmd2, "second delta should not return a command while tick is pending")
	assert.True(t, mm2.renderScheduled, "renderScheduled should still be true")

	// Third delta also should not schedule a tick.
	newM3, cmd3 := mm2.Update(artifactMsg{artifact: artifact.TextDelta{Content: "!"}})
	mm3 := newM3.(*model)
	assert.Nil(t, cmd3, "third delta should not return a command while tick is pending")
	assert.True(t, mm3.renderScheduled, "renderScheduled should still be true")

	// All three deltas should be merged into one block.
	require.Len(t, mm3.currentTurn.blocks, 1)
	assert.Equal(t, "hello world!", mm3.currentTurn.blocks[0].source)
}

// TestModel_Update_AutoScrollLock_PreservesBottom tests that the viewport
// auto-scroll lock (GotoBottom when AtBottom) is preserved across all
// viewport-mutating handlers, not just turnMsg and renderTickMsg.
// Regression test for ore#306.
func TestModel_Update_AutoScrollLock_PreservesBottom(t *testing.T) {
	// Helper: create a model with content tall enough to require scrolling.
	mkModel := func() model {
		m := newTestModel()
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
		m.turns = []renderedTurn{
			{role: state.RoleUser, blocks: []renderedBlock{{title: "You", style: lipgloss.NewStyle(), expandedByDefault: true, kind: "text", source: strings.Repeat("word ", 200)}}},
		}
		m.viewport.SetContent(m.buildContent())
		m.viewport.GotoBottom()
		m.recalcLayout()
		require.True(t, m.viewport.AtBottom(), "model should start at bottom")
		return m
	}

	t.Run("lifecycleMsg submitted", func(t *testing.T) {
		m := mkModel()
		newM, _ := m.Update(lifecycleMsg{phase: "submitted"})
		mm := newM.(*model)
		assert.True(t, mm.viewport.AtBottom(), "lifecycleMsg submitted should preserve bottom lock")
	})

	t.Run("statusMsg", func(t *testing.T) {
		m := mkModel()
		newM, _ := m.Update(statusMsg{status: map[string]string{"foo": "bar"}})
		mm := newM.(*model)
		assert.True(t, mm.viewport.AtBottom(), "statusMsg should preserve bottom lock")
	})

	t.Run("errorMsg", func(t *testing.T) {
		m := mkModel()
		newM, _ := m.Update(errorMsg{err: errors.New("boom")})
		mm := newM.(*model)
		assert.True(t, mm.viewport.AtBottom(), "errorMsg should preserve bottom lock")
	})

	t.Run("clearPendingMsg", func(t *testing.T) {
		m := mkModel()
		m.pending = true // Simulate an in-flight assistant turn.
		newM, _ := m.Update(clearPendingMsg{})
		mm := newM.(*model)
		assert.True(t, mm.viewport.AtBottom(), "clearPendingMsg should preserve bottom lock")
	})

	t.Run("KeyPressMsg Enter with input", func(t *testing.T) {
		m := mkModel()
		m.eventsCh = make(chan session.Event, 10)
		m.textarea.SetValue("hello")
		newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		mm := newM.(*model)
		assert.True(t, mm.viewport.AtBottom(), "KeyEnter with input should preserve bottom lock")
	})

	t.Run("PasteMsg", func(t *testing.T) {
		m := mkModel()
		newM, _ := m.Update(tea.PasteMsg{Content: "pasted text"})
		mm := newM.(*model)
		assert.True(t, mm.viewport.AtBottom(), "PasteMsg should preserve bottom lock")
	})

	t.Run("WindowSizeMsg", func(t *testing.T) {
		m := mkModel()
		newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		mm := newM.(*model)
		assert.True(t, mm.viewport.AtBottom(), "WindowSizeMsg should preserve bottom lock")
	})
}

func TestModel_LoadHistory(t *testing.T) {
	t.Run("empty history", func(t *testing.T) {
		m := newTestModel()
		m.loadHistory(nil)
		assert.Empty(t, m.turns)
		assert.False(t, m.contentDirty)
	})

	t.Run("user turn", func(t *testing.T) {
		m := newTestModel()
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
		m.md = mockMarkdownRenderer{output: "rendered hello world"}
		m.loadHistory([]state.Turn{
			{
				Role:      state.RoleUser,
				Artifacts: []artifact.Artifact{artifact.Text{Content: "hello world"}},
			},
		})
		require.Len(t, m.turns, 1)
		assert.Equal(t, state.RoleUser, m.turns[0].role)
		require.Len(t, m.turns[0].blocks, 1)
		assert.Equal(t, "text", m.turns[0].blocks[0].kind)
		assert.Equal(t, "hello world", m.turns[0].blocks[0].source)
		assert.Equal(t, "rendered hello world", m.turns[0].blocks[0].rendered, "user turn should be markdown rendered")
		assert.True(t, m.contentDirty)
	})

	t.Run("assistant turn with text", func(t *testing.T) {
		m := newTestModel()
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
		m.loadHistory([]state.Turn{
			{
				Role:      state.RoleAssistant,
				Artifacts: []artifact.Artifact{artifact.Text{Content: "response text"}},
			},
		})
		require.Len(t, m.turns, 1)
		assert.Equal(t, state.RoleAssistant, m.turns[0].role)
		require.Len(t, m.turns[0].blocks, 1)
		assert.Equal(t, "text", m.turns[0].blocks[0].kind)
		assert.Equal(t, "response text", m.turns[0].blocks[0].source)
		assert.NotEmpty(t, m.turns[0].blocks[0].rendered, "assistant text should be markdown rendered")
	})

	t.Run("multiple turns in order", func(t *testing.T) {
		m := newTestModel()
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
		m.loadHistory([]state.Turn{
			{
				Role:      state.RoleUser,
				Artifacts: []artifact.Artifact{artifact.Text{Content: "first"}},
			},
			{
				Role:      state.RoleAssistant,
				Artifacts: []artifact.Artifact{artifact.Text{Content: "second"}},
			},
			{
				Role:      state.RoleUser,
				Artifacts: []artifact.Artifact{artifact.Text{Content: "third"}},
			},
		})
		require.Len(t, m.turns, 3)
		assert.Equal(t, state.RoleUser, m.turns[0].role)
		assert.Equal(t, "first", m.turns[0].blocks[0].source)
		assert.Equal(t, state.RoleAssistant, m.turns[1].role)
		assert.Equal(t, "second", m.turns[1].blocks[0].source)
		assert.Equal(t, state.RoleUser, m.turns[2].role)
		assert.Equal(t, "third", m.turns[2].blocks[0].source)
	})

	t.Run("assistant turn with reasoning", func(t *testing.T) {
		m := newTestModel()
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
		m.loadHistory([]state.Turn{
			{
				Role: state.RoleAssistant,
				Artifacts: []artifact.Artifact{
					artifact.Text{Content: "answer"},
					artifact.Reasoning{Content: "thinking..."},
				},
			},
		})
		require.Len(t, m.turns, 1)
		require.Len(t, m.turns[0].blocks, 2)
		assert.Equal(t, "text", m.turns[0].blocks[0].kind)
		assert.Equal(t, "answer", m.turns[0].blocks[0].source)
		assert.Equal(t, "reasoning", m.turns[0].blocks[1].kind)
		assert.Equal(t, "thinking...", m.turns[0].blocks[1].source)
	})

	t.Run("tool turn", func(t *testing.T) {
		m := newTestModel()
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
		m.loadHistory([]state.Turn{
			{
				Role: state.RoleTool,
				Artifacts: []artifact.Artifact{
					artifact.ToolResult{ToolCallID: "call_1", Content: "result data"},
				},
			},
		})
		require.Len(t, m.turns, 1)
		assert.Equal(t, state.RoleTool, m.turns[0].role)
		require.Len(t, m.turns[0].blocks, 1)
		assert.Equal(t, "tool_result", m.turns[0].blocks[0].kind)
	})
}

func TestModel_LoadHistory_WindowSize_Rerenders(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	// Use a recording renderer to capture width values passed to Render.
	rec := &recordingMockRenderer{output: "rendered-at-width"}
	m.md = rec

	// Load history with assistant text.
	m.loadHistory([]state.Turn{
		{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "text that wraps differently at width forty versus width eighty."},
			},
		},
	})

	require.Len(t, m.turns, 1)
	require.Len(t, m.turns[0].blocks, 1)
	// After loadHistory, renderMarkdown was called once at the initial viewport width (80).
	require.Len(t, rec.widths, 1)
	assert.Equal(t, 80, rec.widths[0])
	assert.Equal(t, "rendered-at-width", m.turns[0].blocks[0].rendered)

	// Resize to a narrower width.
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	mm := newM.(*model)

	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	// After WindowSizeMsg, renderMarkdown was called again at width 40.
	require.Len(t, rec.widths, 2)
	assert.Equal(t, 40, rec.widths[1])
	assert.Equal(t, "rendered-at-width", mm.turns[0].blocks[0].rendered)
}

func TestModel_Update_ReloadHistory(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	// Pre-populate with two turns.
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: "hello"}}},
		{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "hi"}}},
	}

	replacementTurn := state.Turn{
		Role: state.RoleSystem,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "compacted summary"},
		},
	}

	newM, _ := m.Update(reloadHistoryMsg{turns: []state.Turn{replacementTurn}})
	mm := newM.(*model)

	require.Len(t, mm.turns, 1)
	assert.Equal(t, state.RoleSystem, mm.turns[0].role)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Equal(t, "text", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "compacted summary", mm.turns[0].blocks[0].source)
	assert.False(t, mm.pending)
	assert.Empty(t, mm.currentTurn.blocks)
}

func TestModel_Update_ReloadHistory_Empty(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: "hello"}}},
	}

	newM, _ := m.Update(reloadHistoryMsg{turns: []state.Turn{}})
	mm := newM.(*model)

	assert.Empty(t, mm.turns)
	assert.False(t, mm.pending)
	assert.Empty(t, mm.currentTurn.blocks)
}

func TestModel_Update_ReloadHistory_PreservesScroll(t *testing.T) {
	// Case 1: at bottom -> stays at bottom.
	m1 := newTestModel()
	m1.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	m1.md = mockMarkdownRenderer{output: "rendered\nline"}
	for i := 0; i < 50; i++ {
		m1.turns = append(m1.turns, renderedTurn{
			role:   state.RoleUser,
			blocks: []renderedBlock{{kind: "text", source: "any"}},
		})
	}
	m1.contentDirty = true
	m1.syncViewport()
	m1.viewport.GotoBottom()
	assert.True(t, m1.viewport.AtBottom(), "should be at bottom before reload")

	replacementTurns := make([]state.Turn, 50)
	for i := 0; i < 50; i++ {
		replacementTurns[i] = state.Turn{
			Role: state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "any"},
			},
		}
	}

	newM1, _ := m1.Update(reloadHistoryMsg{turns: replacementTurns})
	mm1 := newM1.(*model)
	mm1.syncViewport()
	assert.True(t, mm1.viewport.AtBottom(), "should remain at bottom after reload")

	// Case 2: not at bottom -> does not jump to bottom.
	m2 := newTestModel()
	m2.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(5))
	m2.md = mockMarkdownRenderer{output: "rendered\nline"}
	for i := 0; i < 50; i++ {
		m2.turns = append(m2.turns, renderedTurn{
			role:   state.RoleUser,
			blocks: []renderedBlock{{kind: "text", source: "any"}},
		})
	}
	m2.contentDirty = true
	m2.syncViewport()
	// Viewport is at top by default, so AtBottom is false.
	assert.False(t, m2.viewport.AtBottom(), "should not be at bottom initially")

	newM2, _ := m2.Update(reloadHistoryMsg{turns: replacementTurns})
	mm2 := newM2.(*model)
	mm2.syncViewport()
	assert.False(t, mm2.viewport.AtBottom(), "should not jump to bottom when not previously at bottom")
}

func TestModel_Update_ReloadHistory_ClearsRenderScheduled(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.renderScheduled = true

	newM, _ := m.Update(reloadHistoryMsg{turns: []state.Turn{{
		Role: state.RoleUser,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello"},
		},
	}}})
	mm := newM.(*model)

	assert.False(t, mm.renderScheduled, "renderScheduled should be cleared after reload")
}

func TestModel_Update_ReloadHistory_WithPending(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.pending = true

	newM, _ := m.Update(reloadHistoryMsg{turns: []state.Turn{{
		Role: state.RoleUser,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello"},
		},
	}}})
	mm := newM.(*model)

	assert.False(t, mm.pending, "pending should be reset after reload")
}

func TestModel_Update_ReloadHistory_NilSlice(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: "hello"}}},
	}

	newM, _ := m.Update(reloadHistoryMsg{turns: nil})
	mm := newM.(*model)

	assert.Empty(t, mm.turns, "nil slice should be treated as empty history")
	assert.False(t, mm.pending)
	assert.Empty(t, mm.currentTurn.blocks)
}

func TestModel_Update_FeedbackMsg(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.pending = true // simulate an in-flight assistant response

	newM, _ := m.Update(feedbackMsg{content: "Unknown command: /foo"})
	mm := newM.(*model)

	// Should append a system turn with the feedback content.
	require.Len(t, mm.turns, 1)
	assert.Equal(t, state.RoleSystem, mm.turns[0].role)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Equal(t, "feedback", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "Unknown command: /foo", mm.turns[0].blocks[0].source)
	assert.Equal(t, "System", mm.turns[0].blocks[0].title)
	assert.True(t, mm.turns[0].blocks[0].expandedByDefault)

	// Pending should NOT be reset (feedback is not an error).
	assert.True(t, mm.pending, "pending should remain unchanged for feedback")
}

func TestModel_Update_ActivityMsg_SetsWorkingAndDescription(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(activityMsg{active: true, description: "compacting"})
	mm := newM.(*model)
	assert.True(t, mm.working, "activityMsg should set working=true")
	assert.Equal(t, "compacting", mm.workingDescription, "activityMsg should set description")

	// Turn off activity
	newM2, _ := mm.Update(activityMsg{active: false, description: ""})
	mm2 := newM2.(*model)
	assert.False(t, mm2.working, "activityMsg should clear working")
	assert.Empty(t, mm2.workingDescription, "activityMsg should clear description")
}
