// model.go implements the Bubble Tea model used by the TUI conduit.
// It receives turn notifications from the ore core and updates the
// on-screen conversation view, including a pending placeholder while an
// assistant response is in flight.
package tui

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// artifactMsg is a Bubble Tea message that carries a single artifact
// from an ArtifactEvent into the model.Update loop so it can be
// appended incrementally to the current assistant turn before the
// TurnCompleteEvent boundary arrives.
type artifactMsg struct {
	artifact artifact.Artifact
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
	status map[string]string
}

// clearPendingMsg is a Bubble Tea message that instructs the model to
// clear the pending flag, typically because the manager failed to produce
// a turn.
type clearPendingMsg struct{}

// audioMsg tells the UI model to emit a terminal bell. Using a message
// keeps the side-effect on the Bubble Tea UI goroutine, preserving
// thread safety. The bell cannot vary pitch, so done and error tones
// are identical; a richer audio backend would need a distinct message.
type audioMsg struct{}

// errorMsg carries an error from the conduit to the UI model. The model
// clears the pending state and updates the status line so the user sees
// the failure immediately. Audio feedback is handled separately via
// audioMsg sent by PlayError.
type errorMsg struct {
	err error
}

// renderTickMsg triggers a debounced markdown re-render of the current
// assistant turn's text and reasoning blocks.
type renderTickMsg struct{}

// renderedBlock tracks a finalized piece of turn content with its kind,
// original source, optional compact representation, and optional
// pre-rendered ANSI cache.
// compact holds the one-line summary used when the UI is in compact mode
// (Ctrl+O collapsed). It is computed during turn processing to avoid
// repeated JSON parsing on every viewport refresh.
type renderedBlock struct {
	kind     string // "text", "reasoning", "tool_call", or "tool_result"
	source   string // original content
	compact  string // compact single-line representation
	rendered string // pre-rendered ANSI output (only for text blocks)
}

// The TUI aims to keep the conversation view concise. Tool calls and their
// results can be verbose, so we render them in a one-line "compact" form by
// default. Users can press Ctrl+O to temporarily expand the latest assistant
// turn's tool interactions, inspecting full arguments or error messages.
// The state is scoped to the most recent turn to avoid cluttering older
// history.
//
// model implements tea.Model. All state mutation happens in Update,
// which runs on Bubble Tea's single goroutine, so no locks are needed.
type model struct {
	eventsCh chan session.Event

	// Conversation history.
	turns []renderedTurn

	// currentTurn accumulates renderedBlock values from ArtifactEvent
	// messages for the in-progress assistant turn. It is flushed into
	// turns when the TurnCompleteEvent boundary arrives.
	currentTurn renderedTurn

	// pending indicates an assistant response is in flight.
	pending bool

	// expandLatestDetails controls whether the latest assistant turn's
	// details (tool calls, tool results, and reasoning) are shown expanded
	// or compact.
	// The flag is toggled by Ctrl+O and automatically cleared after
	// the next assistant turn is received, restoring the default compact view.
	expandLatestDetails bool

	// Status map carries structured key-value status pairs received from
	// StatusEvent output events (e.g. thread_id, state).
	status map[string]string

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

	// cachedContent holds the last computed viewport content string.
	// It is invalidated by setting contentDirty = true.
	cachedContent string

	// contentDirty is true when cachedContent is stale and needs rebuilding.
	contentDirty bool

	// renderScheduled is true when a debounced render tick is pending.
	renderScheduled bool
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
		m.md = newGlamourMarkdownRenderer()
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
	// Reserve space for one or two separators plus the status bar when
	// status has non-empty content.
	_, statusLines := buildStatusLine(m.status, m.width)
	separatorCount := 1
	if statusLines > 0 {
		separatorCount = 2
	}

	m.viewport.SetHeight(m.height - m.textarea.Height() - separatorCount - statusLines)
	if m.viewport.Height() < 1 {
		m.viewport.SetHeight(1)
	}
}

// syncViewport rebuilds the cached content string if stale and pushes it to
// the viewport. Call this from Update() after any mutation that affects
// visual output.
func (m *model) syncViewport() {
	m.viewport.SetContent(m.buildContent())
}

// renderArtifact creates a renderedBlock from a single artifact.Artifact.
// It is used both for incremental ArtifactEvent accumulation (assistant
// turns) and for building user/tool turns from TurnCompleteEvent.
// When shouldRenderMarkdown is true, text and reasoning artifacts are
// processed through the Markdown renderer; this is only appropriate for
// assistant turns.
func (m *model) renderArtifact(art artifact.Artifact, shouldRenderMarkdown bool) renderedBlock {
	switch a := art.(type) {
	case artifact.Text:
		block := renderedBlock{kind: "text", source: a.Content}
		if shouldRenderMarkdown {
			rendered, err := m.renderMarkdown(a.Content, m.viewport.Width())
			if err == nil {
				block.rendered = rendered
			}
		}
		return block
	case artifact.Reasoning:
		block := renderedBlock{kind: "reasoning", source: a.Content, compact: "Thinking..."}
		if shouldRenderMarkdown {
			rendered, err := m.renderMarkdown(a.Content, m.viewport.Width())
			if err == nil {
				block.rendered = rendered
			}
		}
		return block
	case artifact.ToolCall:
		source := fmt.Sprintf("Calling: %s(%s)", a.Name, a.Arguments)
		compact := compactToolCall(a, m.viewport.Width())
		return renderedBlock{kind: "tool_call", source: source, compact: compact}
	case artifact.ToolResult:
		source := a.Content
		if a.IsError {
			source = "Error: " + source
		}
		compact := compactToolResult(a, m.viewport.Width())
		return renderedBlock{kind: "tool_result", source: source, compact: compact}
	}
	return renderedBlock{}
}

// Init returns an initial command. No periodic ticks are needed because
// turns arrive via program.Send from the orchestrator goroutine.
func (m *model) Init() tea.Cmd {
	return nil
}

// Update handles incoming messages: keyboard input, window resize, and
// custom messages carrying delta/turn/status data from the conduit methods.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case artifactMsg:
		switch a := msg.artifact.(type) {
		case artifact.TextDelta:
			if a.Content == "" {
				return m, nil
			}
			found := false
			for i := len(m.currentTurn.blocks) - 1; i >= 0; i-- {
				if m.currentTurn.blocks[i].kind == "text" {
					m.currentTurn.blocks[i].source += a.Content
					m.currentTurn.blocks[i].rendered = ""
					found = true
					break
				}
			}
			if !found {
				m.currentTurn.blocks = append(m.currentTurn.blocks, renderedBlock{kind: "text", source: a.Content})
			}
		case artifact.ReasoningDelta:
			if a.Content == "" {
				return m, nil
			}
			found := false
			for i := len(m.currentTurn.blocks) - 1; i >= 0; i-- {
				if m.currentTurn.blocks[i].kind == "reasoning" {
					m.currentTurn.blocks[i].source += a.Content
					m.currentTurn.blocks[i].rendered = ""
					found = true
					break
				}
			}
			if !found {
				m.currentTurn.blocks = append(m.currentTurn.blocks, renderedBlock{kind: "reasoning", source: a.Content, compact: "Thinking..."})
			}
		default:
			block := m.renderArtifact(msg.artifact, true)
			if block.kind != "" {
				m.currentTurn.blocks = append(m.currentTurn.blocks, block)
			}
		}
		m.contentDirty = true
		if !m.renderScheduled {
			m.renderScheduled = true
			return m, tea.Tick(16*time.Millisecond, func(t time.Time) tea.Msg {
				return renderTickMsg{}
			})
		}
	case turnMsg:
		if msg.turn.Role == state.RoleAssistant {
			// Cancel any pending render tick.
			m.renderScheduled = false

			// Final render pass on the accumulated blocks before
			// moving them into the permanent conversation history.
			for j := range m.currentTurn.blocks {
				block := &m.currentTurn.blocks[j]
				if (block.kind == "text" || block.kind == "reasoning") && block.source != "" {
					rendered, err := m.renderMarkdown(block.source, m.viewport.Width())
					if err == nil {
						block.rendered = rendered
					}
				}
			}

			// Finalize the currentTurn that was built incrementally from
			// ArtifactEvents. If no ArtifactEvents arrived (empty response),
			// currentTurn may be empty; we still record the turn boundary.
			m.currentTurn.role = msg.turn.Role
			m.turns = append(m.turns, m.currentTurn)
			m.currentTurn = renderedTurn{}
			m.pending = false
			m.expandLatestDetails = false
		} else {
			// User and tool turns do not emit individual ArtifactEvents;
			// build the turn from the full Turn content.
			var blocks []renderedBlock
			for _, art := range msg.turn.Artifacts {
				block := m.renderArtifact(art, msg.turn.Role == state.RoleAssistant)
				if block.kind != "" {
					blocks = append(blocks, block)
				}
			}
			rt := renderedTurn{
				role:   msg.turn.Role,
				blocks: blocks,
			}
			m.turns = append(m.turns, rt)
		}
		m.contentDirty = true
		m.syncViewport()
		m.viewport.GotoBottom()
	case statusMsg:
		if m.status == nil {
			m.status = make(map[string]string)
		}
		for k, v := range msg.status {
			m.status[k] = v
		}
		m.recalcLayout()
		m.syncViewport()
	case clearPendingMsg:
		m.pending = false
		m.contentDirty = true
		m.syncViewport()
	case audioMsg:
		// Emit the terminal bell as an audible cue. The bell is the
		// same for done and error because \a cannot vary pitch.
		fmt.Print("\a")
		return m, nil
	case errorMsg:
		// Surface the error to the user and reset UI state so the
		// input area is usable again. Discard any partial assistant
		// output accumulated in currentTurn.
		m.currentTurn = renderedTurn{}
		m.pending = false
		m.renderScheduled = false
		if m.status == nil {
			m.status = make(map[string]string)
		}
		m.status["state"] = "Error: " + msg.err.Error()
		m.contentDirty = true
		m.recalcLayout()
		m.syncViewport()
		return m, nil
	case renderTickMsg:
		if !m.renderScheduled {
			return m, nil
		}
		for j := range m.currentTurn.blocks {
			block := &m.currentTurn.blocks[j]
			if (block.kind == "text" || block.kind == "reasoning") && block.source != "" {
				rendered, err := m.renderMarkdown(block.source, m.viewport.Width())
				if err == nil {
					block.rendered = rendered
				}
			}
		}
		m.syncViewport()
		m.viewport.GotoBottom()
		m.contentDirty = false
		m.renderScheduled = false
		if m.contentDirty {
			m.renderScheduled = true
			return m, tea.Tick(16*time.Millisecond, func(t time.Time) tea.Msg {
				return renderTickMsg{}
			})
		}
	case tea.KeyPressMsg:
		switch msg.Key().Code {
		case tea.KeyPgUp, tea.KeyPgDown:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case tea.KeyEnter:
			if !msg.Key().Mod.Contains(tea.ModShift) {
				if m.textarea.Value() != "" {
					content := m.textarea.Value()
					m.textarea.Reset()
					m.recalcLayout()
					select {
					case m.eventsCh <- session.UserMessageEvent{Content: content}:
					default:
						slog.Warn("event channel full, dropping user message")
					}
					m.pending = true
					m.contentDirty = true
					m.syncViewport()
				}
				return m, nil
			}
			// Shift+Enter: pass to textarea for newline insertion.
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.recalcLayout()
			return m, cmd
		}

		// Ctrl+C
		if msg.Key().Code == 'c' && msg.Key().Mod.Contains(tea.ModCtrl) {
			select {
			case m.eventsCh <- session.InterruptEvent{}:
			default:
			}
			return m, tea.Quit
		}

		// Space
		if msg.Key().Code == tea.KeySpace {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.recalcLayout()
			return m, cmd
		}

		// Ctrl+O
		if msg.Key().Code == 'o' && msg.Key().Mod.Contains(tea.ModCtrl) {
			m.expandLatestDetails = !m.expandLatestDetails
			m.contentDirty = true
			m.syncViewport()
			m.viewport.GotoBottom()
			return m, nil
		}

		// Default: pass to textarea
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.recalcLayout()
		return m, cmd
	case tea.PasteMsg:
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.recalcLayout()
		return m, cmd
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(msg.Width)
		m.textarea.SetWidth(msg.Width)
		m.recalcLayout()
		// Re-render assistant turn text blocks with the new terminal width
		// so cached Markdown output remains correctly wrapped.
		for i, turn := range m.turns {
			if turn.role == state.RoleAssistant {
				for j, block := range turn.blocks {
					if block.kind == "text" && block.source != "" {
						rendered, err := m.renderMarkdown(block.source, m.viewport.Width())
						if err == nil {
							m.turns[i].blocks[j].rendered = rendered
						}
					}
				}
			}
		}
		// Also re-render the in-progress currentTurn blocks.
		for j, block := range m.currentTurn.blocks {
			if block.kind == "text" && block.source != "" {
				rendered, err := m.renderMarkdown(block.source, m.viewport.Width())
				if err == nil {
					m.currentTurn.blocks[j].rendered = rendered
				}
			}
		}
		m.renderScheduled = false
		m.contentDirty = true
		m.syncViewport()
	}
	return m, nil
}
