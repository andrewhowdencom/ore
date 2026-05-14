package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/conduit"
	"github.com/andrewhowdencom/ore/state"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestModel returns a model with a properly initialized textarea widget.
// Tests that send key messages or call View() must use this helper to avoid
// panics from the zero-value textarea.
func newTestModel() model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))
	ta.Focus()
	return model{
		textarea: ta,
	}
}

func TestModel_Update_Turn(t *testing.T) {
	m := model{}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello world"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	assert.Equal(t, state.RoleAssistant, mm.turns[0].role)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Equal(t, "text", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "hello world", mm.turns[0].blocks[0].source)
}

func TestModel_Update_Turn_PreservesReasoning(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
	}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "the answer is 42"},
			artifact.Reasoning{Content: "let me think..."},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 2)
	assert.Equal(t, "text", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "the answer is 42", mm.turns[0].blocks[0].source)
	assert.Equal(t, "reasoning", mm.turns[0].blocks[1].kind)
	assert.Equal(t, "let me think...", mm.turns[0].blocks[1].source)
}

func TestModel_Update_Turn_ClearsPending(t *testing.T) {
	m := model{}
	m.pending = true

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "complete"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	assert.False(t, mm.pending)
}

func TestModel_Update_Turn_Interleaved(t *testing.T) {
	m := model{}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "Hello"},
			artifact.Reasoning{Content: "think"},
			artifact.Text{Content: " world"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 3)
	assert.Equal(t, "text", mm.turns[0].blocks[0].kind)
	assert.Equal(t, "Hello", mm.turns[0].blocks[0].source)
	assert.Equal(t, "reasoning", mm.turns[0].blocks[1].kind)
	assert.Equal(t, "think", mm.turns[0].blocks[1].source)
	assert.Equal(t, "text", mm.turns[0].blocks[2].kind)
	assert.Equal(t, " world", mm.turns[0].blocks[2].source)
}

func TestModel_Update_Status(t *testing.T) {
	m := model{}
	newM, _ := m.Update(statusMsg{status: "thinking..."})
	mm := newM.(*model)
	assert.Equal(t, "thinking...", mm.status)
}

func TestModel_Update_KeyRunes(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	mm := newM.(*model)
	assert.Equal(t, "hello", mm.textarea.Value())
}

func TestModel_Update_KeySpace(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	mm := newM.(*model)
	assert.Equal(t, " ", mm.textarea.Value())
}

func TestModel_Update_KeyBackspace(t *testing.T) {
	m := newTestModel()
	m.textarea.SetValue("hi")
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm := newM.(*model)
	assert.Equal(t, "h", mm.textarea.Value())
}

func TestModel_Update_KeyBackspace_Empty(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm := newM.(*model)
	assert.Empty(t, mm.textarea.Value())
}

func TestModel_Update_KeyEnter_WithInput(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("hello")

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := newM.(*model)

	// User turns no longer render directly on KeyEnter; they arrive via
	// turnMsg from the loop's FanOut.
	assert.Empty(t, mm.turns)
	assert.Empty(t, mm.textarea.Value())

	select {
	case e := <-eventsCh:
		require.Equal(t, "user_message", e.Kind())
		ume, ok := e.(conduit.UserMessageEvent)
		require.True(t, ok)
		assert.Equal(t, "hello", ume.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestModel_Update_KeyEnter_EmptyInput(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	assert.Equal(t, 80, mm.viewport.Width)
	assert.Equal(t, 22, mm.viewport.Height)
}

func TestModel_Update_KeyCtrlC(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
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
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: "hello"}}},
	}
	output := m.View()
	assert.Contains(t, output, "You: ")
	assert.Contains(t, output, "hello")
	idxLabel := strings.Index(output, "You: ")
	idxContent := strings.Index(output, "hello")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Contains(t, segment, "\n", "label and content should be on separate lines")
}

func TestModel_View_ContainsAssistantTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleAssistant, blocks: []renderedBlock{{kind: "text", source: "world"}}},
	}
	output := m.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "world")
	idxLabel := strings.Index(output, "Assistant: ")
	idxContent := strings.Index(output, "world")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Contains(t, segment, "\n", "label and content should be on separate lines")
}

func TestModel_View_ContainsToolTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleTool, blocks: []renderedBlock{{kind: "text", source: "result"}}},
	}
	output := m.View()
	assert.Contains(t, output, "Tool: ")
	assert.Contains(t, output, "result")
	idxLabel := strings.Index(output, "Tool: ")
	idxContent := strings.Index(output, "result")
	assert.Greater(t, idxContent, idxLabel, "content should appear after label")
	segment := output[idxLabel:idxContent]
	assert.Contains(t, segment, "\n", "label and content should be on separate lines")
}

func TestModel_View_ContainsStatus(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.status = "thinking..."
	output := m.View()
	assert.Contains(t, output, "thinking...")
}

func TestModel_View_ContainsPrompt(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.textarea.SetValue("hi")
	output := m.View()
	assert.Contains(t, output, "> ")
	assert.Contains(t, output, "hi")
}

func TestModel_View_Empty(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	output := m.View()
	assert.Contains(t, output, "> ")
}

func TestModel_View_ContainsInputAtBottom(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: "hello"}}},
	}
	output := m.View()
	lines := strings.Split(output, "\n")
	lastLine := lines[len(lines)-1]
	assert.Contains(t, lastLine, "> ")
}

func TestModel_Update_PgUp_ScrollsViewport(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 5)
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()
	initialYOffset := m.viewport.YOffset

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	mm := newM.(*model)

	assert.Less(t, mm.viewport.YOffset, initialYOffset, "PgUp should scroll viewport up")
}

func TestModel_Update_PgDown_ScrollsViewport(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 5)
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()

	// Scroll up first so PgDown has room to scroll back down
	m.viewport.HalfPageUp()
	initialYOffset := m.viewport.YOffset

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	mm := newM.(*model)

	assert.Greater(t, mm.viewport.YOffset, initialYOffset, "PgDown should scroll viewport down")
}

func TestModel_Update_Turn_AutoScrollsViewport(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 5)
	m.viewport.SetContent(strings.Repeat("line\n", 20))
	m.viewport.GotoBottom()

	// Scroll up to simulate user reading history
	m.viewport.HalfPageUp()
	assert.False(t, m.viewport.AtBottom(), "should not be at bottom after scrolling up")

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello world"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)

	assert.True(t, mm.viewport.AtBottom(), "turn should auto-scroll viewport to bottom")
}

func TestModel_View_LongHistory_InputAtBottom(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 5)
	// Add enough turns to exceed viewport height
	for i := 0; i < 10; i++ {
		m.turns = append(m.turns, renderedTurn{
			role:   state.RoleUser,
			blocks: []renderedBlock{{kind: "text", source: strings.Repeat("word ", 20)}},
		})
	}
	output := m.View()
	lines := strings.Split(output, "\n")
	lastLine := lines[len(lines)-1]
	assert.Contains(t, lastLine, "> ")
}

func TestModel_View_WrapsLongTurn(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(20, 5)
	m.turns = []renderedTurn{
		{role: state.RoleUser, blocks: []renderedBlock{{kind: "text", source: strings.Repeat("word ", 10)}}},
	}
	output := m.View()
	lines := strings.Split(output, "\n")
	// Find the label line
	labelIdx := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "You: ") {
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
		viewport: viewport.New(80, 20),
	}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "# Hello\n\n**bold** text"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.NotEmpty(t, mm.turns[0].blocks[0].source)
	assert.NotEmpty(t, mm.turns[0].blocks[0].rendered, "assistant turn should have rendered Markdown")
}

func TestModel_Update_Turn_User_LeavesRenderedEmpty(t *testing.T) {
	m := model{
		viewport: viewport.New(80, 20),
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
	assert.Empty(t, mm.turns[0].blocks[0].rendered, "user turn should not have rendered Markdown")
}

func TestModel_Update_WindowSize_RerendersAssistantTurns(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "# Title\n\nThis is a longer paragraph that should definitely wrap differently at width forty versus width eighty."},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	initialRendered := mm.turns[0].blocks[0].rendered
	assert.NotEmpty(t, initialRendered)

	// Resize to a narrower width
	newM2, _ := mm.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	mm2 := newM2.(*model)
	assert.NotEmpty(t, mm2.turns[0].blocks[0].rendered)
	assert.NotEqual(t, initialRendered, mm2.turns[0].blocks[0].rendered,
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
	m.viewport = viewport.New(80, 20)
	m.md = mockMarkdownRenderer{err: errors.New("render failed")}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "# Hello"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Empty(t, mm.turns[0].blocks[0].rendered, "render error should leave rendered empty")
	assert.Equal(t, "# Hello", mm.turns[0].blocks[0].source, "raw text should still be stored")
}

func TestModel_View_AssistantTurn_RenderError_FallbackToPlainText(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.md = mockMarkdownRenderer{err: errors.New("render failed")}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "plain fallback text"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	output := mm.View()
	assert.Contains(t, output, "Assistant: ")
	assert.Contains(t, output, "plain fallback text")
}

func TestModel_Update_WindowSize_RerenderError_KeepsOldCache(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.md = mockMarkdownRenderer{output: "initial-render"}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "text"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	assert.Equal(t, "initial-render", mm.turns[0].blocks[0].rendered)

	// Swap to an error-returning renderer and resize.
	mm.md = mockMarkdownRenderer{err: errors.New("resize render failed")}
	newM2, _ := mm.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	mm2 := newM2.(*model)
	assert.Equal(t, "initial-render", mm2.turns[0].blocks[0].rendered,
		"old cache should be kept on re-render error")
}

func TestModel_Update_AltEnter_InsertsNewline(t *testing.T) {
	m := newTestModel()
	m.textarea.SetValue("hello")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	mm := newM.(*model)

	assert.Contains(t, mm.textarea.Value(), "\n")
}

func TestModel_Update_Enter_SubmitsMultiLine(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("line1\nline2")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := newM.(*model)

	assert.Empty(t, mm.textarea.Value())

	select {
	case e := <-eventsCh:
		require.Equal(t, "user_message", e.Kind())
		ume, ok := e.(conduit.UserMessageEvent)
		require.True(t, ok)
		assert.Equal(t, "line1\nline2", ume.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestModel_View_ContainsSeparator(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.width = 80
	output := m.View()
	assert.Contains(t, output, strings.Repeat("─", 80))
}

func TestModel_Update_Turn_Assistant_EmptyText(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.md = mockMarkdownRenderer{output: "mock-empty-output"}
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: ""},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	require.Len(t, mm.turns, 1)
	require.Len(t, mm.turns[0].blocks, 1)
	assert.Empty(t, mm.turns[0].blocks[0].source)
	assert.Equal(t, "mock-empty-output", mm.turns[0].blocks[0].rendered)
	// View should not crash with empty text.
	output := mm.View()
	assert.Contains(t, output, "Assistant: ")
}

// --- Critical coverage gap tests (added per testing agent review) ---

func TestModel_Update_AltEnter_DoesNotEmitEvent(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("hello")
	m.recalcLayout()

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	mm := newM.(*model)

	assert.Contains(t, mm.textarea.Value(), "\n")

	select {
	case <-eventsCh:
		t.Fatal("Alt+Enter should not emit a UserMessageEvent")
	default:
	}
}

func TestModel_Update_DynamicLayout(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := newM.(*model)

	// Empty textarea: 1 line, separator: 1 line, viewport: 22 lines
	assert.Equal(t, 1, mm.textarea.Height(), "empty textarea should be 1 line")
	assert.Equal(t, 22, mm.viewport.Height, "viewport should fill remaining space")

	// Add 3 lines
	mm.textarea.SetValue("line1\nline2\nline3")
	mm.recalcLayout()

	assert.Equal(t, 3, mm.textarea.Height(), "textarea should grow to 3 lines")
	assert.Equal(t, 20, mm.viewport.Height, "viewport should shrink accordingly")

	// Add many lines to hit the cap: max(3, 24/3) = 8
	mm.textarea.SetValue(strings.Repeat("x\n", 20))
	mm.recalcLayout()

	assert.Equal(t, 8, mm.textarea.Height(), "should respect max height cap")
	assert.Equal(t, 15, mm.viewport.Height, "viewport should shrink to minimum")
}

func TestModel_View_SeparatorAdaptsToResize(t *testing.T) {
	m := newTestModel()
	m.viewport = viewport.New(80, 20)
	m.width = 80
	m.status = "ready" // ensure viewport has content so separator is rendered
	output := m.View()
	assert.Contains(t, output, strings.Repeat("─", 80))

	// Resize to narrower width
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	mm := newM.(*model)
	mm.status = "ready"
	output = mm.View()
	assert.Contains(t, output, strings.Repeat("─", 50))
}

func TestModel_Update_AltEnter_EmptyTextarea(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	mm := newM.(*model)

	assert.Equal(t, "\n", mm.textarea.Value())

	select {
	case <-eventsCh:
		t.Fatal("Alt+Enter on empty textarea should not emit event")
	default:
	}
}

func TestModel_Update_RecalcLayout_MinimumViewportHeight(t *testing.T) {
	m := newTestModel()
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 2})
	mm := newM.(*model)

	// Even with a tiny terminal, viewport should never collapse to 0
	assert.GreaterOrEqual(t, mm.viewport.Height, 1, "viewport height should never be < 1")
}

func TestModel_Update_KeyEnter_SetsPending(t *testing.T) {
	eventsCh := make(chan conduit.Event, 10)
	m := newTestModel()
	m.eventsCh = eventsCh
	m.textarea.SetValue("hello")

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := newM.(*model)

	assert.True(t, mm.pending, "KeyEnter should set pending=true")
}

func TestModel_Update_Turn_Assistant_ClearsPending(t *testing.T) {
	m := model{}
	m.pending = true

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "response"},
		},
	}
	newM, _ := m.Update(turnMsg{turn: turn})
	mm := newM.(*model)
	assert.False(t, mm.pending, "assistant turn should clear pending")
}

func TestModel_Update_Turn_User_DoesNotClearPending(t *testing.T) {
	m := model{}
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
