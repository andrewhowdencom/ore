// model.go implements the Bubble Tea model used by the TUI conduit.
// It receives streaming artifacts and turn notifications from the
// ore core and updates the on-screen conversation view.
package tui

import (
	"log/slog"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/conduit"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// deltaMsg is a Bubble Tea message that carries an ephemeral streaming
// delta artifact into the model.Update loop so it can be appended to
// the appropriate streaming buffer (text or reasoning).
type deltaMsg struct {
	delta artifact.Artifact
}

// turnMsg is a Bubble Tea message that carries a complete turn into
// the model.Update loop so it can be finalized in the conversation
// history.
type turnMsg struct {
	turn state.Turn
}

// statusMsg is a Bubble Tea message that carries a status update into
// the model.Update loop so it can update the transient status line.
type statusMsg struct {
	status string
}

// streamBlock tracks an ordered piece of streaming content with its kind.
type streamBlock struct {
	kind    string // "text" or "reasoning"
	content string
}

// renderedBlock tracks a finalized piece of turn content with its kind and
// optional pre-rendered ANSI cache.
type renderedBlock struct {
	kind     string // "text" or "reasoning"
	source   string // original content
	rendered string // pre-rendered ANSI output (only for text blocks)
}

// model implements tea.Model. All state mutation happens in Update,
// which runs on Bubble Tea's single goroutine, so no locks are needed.
type model struct {
	eventsCh chan conduit.Event

	// Conversation history.
	turns []renderedTurn

	// streamBlocks holds the ordered partial content of the current assistant
	// response. Each block is either text or reasoning, preserving the
	// arrival order from the provider.
	streamBlocks []streamBlock

	// Transient status line (e.g., "thinking...").
	status string

	// User input widget.
	textarea textarea.Model

	// Terminal dimensions.
	width  int
	height int

	// Scrollable viewport for conversation history.
	viewport viewport.Model

	// md renders Markdown source into ANSI-styled terminal output. In
	// production this is a glamourMarkdownRenderer; tests may inject a mock.
	md markdownRenderer
}

// renderedTurn represents a single turn in the conversation history.
type renderedTurn struct {
	role   state.Role
	blocks []renderedBlock
}

// renderMarkdown delegates to the model's markdown renderer, falling back
// to a default glamourMarkdownRenderer if none was injected.
func (m *model) renderMarkdown(text string, width int) (string, error) {
	// If no renderer was supplied (e.g. in tests), fall back to the
	// production glamour renderer.
	if m.md == nil {
		m.md = glamourMarkdownRenderer{}
	}
	return m.md.Render(text, width)
}

// recalcLayout adjusts the textarea height based on its current content
// and resizes the viewport to fill the remaining terminal space above the
// horizontal separator.
func (m *model) recalcLayout() {
	if m.height == 0 {
		return
	}

	value := m.textarea.Value()
	contentWidth := m.textarea.Width()
	if contentWidth <= 0 {
		contentWidth = m.width
	}
	if contentWidth <= 0 {
		contentWidth = 80
	}

	logicalLines := strings.Split(value, "\n")
	displayLines := 0
	for _, line := range logicalLines {
		if line == "" {
			displayLines++
		} else {
			// Rough estimate of wrapped lines.
			wrappedLineCount := len(line)/contentWidth + 1
			displayLines += wrappedLineCount
		}
	}

	maxHeight := max(3, m.height/3)
	desiredHeight := min(displayLines, maxHeight)
	desiredHeight = max(desiredHeight, 1)

	m.textarea.SetHeight(desiredHeight)
	m.viewport.Height = m.height - m.textarea.Height() - 1 // -1 for separator
	if m.viewport.Height < 1 {
		m.viewport.Height = 1
	}
}

// Init returns an initial command. No periodic ticks are needed because
// deltas arrive via program.Send from the orchestrator goroutine.
func (m *model) Init() tea.Cmd {
	return nil
}

// Update handles incoming messages: keyboard input, window resize, and
// custom messages carrying delta/turn/status data from the conduit methods.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case deltaMsg:
		switch d := msg.delta.(type) {
		case artifact.TextDelta:
			if len(m.streamBlocks) > 0 && m.streamBlocks[len(m.streamBlocks)-1].kind == "text" {
				m.streamBlocks[len(m.streamBlocks)-1].content += d.Content
			} else {
				m.streamBlocks = append(m.streamBlocks, streamBlock{kind: "text", content: d.Content})
			}
		case artifact.ReasoningDelta:
			if len(m.streamBlocks) > 0 && m.streamBlocks[len(m.streamBlocks)-1].kind == "reasoning" {
				m.streamBlocks[len(m.streamBlocks)-1].content += d.Content
			} else {
				m.streamBlocks = append(m.streamBlocks, streamBlock{kind: "reasoning", content: d.Content})
			}
		}
		m.viewport.GotoBottom()
	case turnMsg:
		var blocks []renderedBlock
		for _, art := range msg.turn.Artifacts {
			switch a := art.(type) {
			case artifact.Text:
				block := renderedBlock{kind: "text", source: a.Content}
				if msg.turn.Role == state.RoleAssistant {
					rendered, err := m.renderMarkdown(a.Content, m.viewport.Width)
					if err == nil {
						block.rendered = rendered
					}
				}
				blocks = append(blocks, block)
			case artifact.Reasoning:
				blocks = append(blocks, renderedBlock{kind: "reasoning", source: a.Content})
			}
		}
		rt := renderedTurn{
			role:   msg.turn.Role,
			blocks: blocks,
		}
		m.turns = append(m.turns, rt)
		m.streamBlocks = nil
		m.viewport.GotoBottom()
	case statusMsg:
		m.status = msg.status
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyPgUp, tea.KeyPgDown:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case tea.KeyEnter:
			if !msg.Alt {
				if m.textarea.Value() != "" {
					content := m.textarea.Value()
					m.textarea.Reset()
					m.recalcLayout()
					select {
					case m.eventsCh <- conduit.UserMessageEvent{Content: content}:
					default:
						slog.Warn("event channel full, dropping user message")
					}
				}
				return m, nil
			}
			// Alt+Enter: pass to textarea for newline insertion.
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.recalcLayout()
			return m, cmd
		case tea.KeyCtrlC:
			select {
			case m.eventsCh <- conduit.InterruptEvent{}:
			default:
			}
			return m, tea.Quit
		case tea.KeySpace:
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
			m.recalcLayout()
			return m, cmd
		default:
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.recalcLayout()
			return m, cmd
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.textarea.SetWidth(msg.Width)
		m.recalcLayout()
		// Re-render assistant turn text blocks with the new terminal width
		// so cached Markdown output remains correctly wrapped.
		for i, turn := range m.turns {
			if turn.role == state.RoleAssistant {
				for j, block := range turn.blocks {
					if block.kind == "text" && block.source != "" {
						rendered, err := m.renderMarkdown(block.source, m.viewport.Width)
						if err == nil {
							m.turns[i].blocks[j].rendered = rendered
						}
					}
				}
			}
		}
	}
	return m, nil
}
