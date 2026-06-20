// model.go implements the Bubble Tea model used by the TUI conduit.
// It receives turn notifications from the ore core and updates the
// on-screen conversation view, including a pending placeholder while an
// assistant response is in flight.
package tui

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/andrewhowdencom/ore/x/conduit/tui/theme"
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

// lifecycleMsg is a Bubble Tea message that carries a LifecycleEvent
// phase into the model.Update loop so the UI can reflect structural
// turn boundaries (submitted, streaming, done).
type lifecycleMsg struct {
	phase string
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

// noticeMsg carries an ephemeral Notice from the conduit to the UI
// model. Notice messages are rendered as system-styled turns in the
// conversation (styled by severity via the theme) but are not
// persisted to state and never reach the LLM.
type noticeMsg struct {
	notice loop.Notice
}

// activityMsg carries an activity indicator update from the conduit to
// the UI model. It signals when long-running work starts and stops.
type activityMsg struct {
	active      bool
	description string
}

// renderTickMsg triggers a debounced markdown re-render of the current
// assistant turn's text and reasoning blocks.
type renderTickMsg struct{}

// reloadHistoryMsg is a Bubble Tea message that instructs the model to
// discard its current turn history and rebuild it from a fresh slice of
// state.Turn values. This is used after compaction (or any other
// operation that replaces the persistent state via stream.LoadTurns) so
// the TUI view remains synchronized with the backend.
type reloadHistoryMsg struct {
	turns []state.Turn
}

// renderedBlock tracks a finalized piece of turn content with its kind,
// original source, optional compact representation, and optional
// pre-rendered ANSI cache.
// compact holds the one-line summary used when the UI is in compact mode
// (Ctrl+O collapsed). It is computed during turn processing to avoid
// repeated JSON parsing on every viewport refresh.
type renderedBlock struct {
	kind              string         // "text", "reasoning", "tool_call", or "tool_result"
	source            string         // original content
	compact           string         // compact single-line representation
	rendered          string         // pre-rendered ANSI output (for text, reasoning, tool_call and tool_result blocks)
	toolCallID        string         // ID pairing tool_call with its corresponding tool_result
	title             string         // display title for the unified header
	style             lipgloss.Style // color/style applied to the header
	expandedByDefault bool           // whether this block type defaults to expanded body
}

// rerenderableKinds lists block kinds that may be re-rendered when the
// viewport width changes or a render tick fires.  Centralized so all
// update handlers use the same set.
var rerenderableKinds = []string{"text", "reasoning", "tool_call", "tool_result"}

// isRerenderableKind reports whether a block kind should be re-rendered
// on width changes or debounced render ticks.
func isRerenderableKind(kind string) bool {
	for _, k := range rerenderableKinds {
		if k == kind {
			return true
		}
	}
	return false
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

	// expandAllDetails controls whether all non-text blocks across all
	// turns (tool calls, tool results, and reasoning) are shown expanded
	// or compact. The flag is toggled by Ctrl+O and persists across new
	// turns and conversation boundaries, so the user's chosen view is
	// preserved until they explicitly change it.
	expandAllDetails bool

	// Status map carries structured key-value metadata pairs received from
	// PropertiesEvent output events (e.g. thread_id, state).
	status map[string]string

	// initStatusMsg is a one-shot status seed produced by initModel from
	// stream.AllMetadata(). It is yielded by Init() as a tea.Cmd so the
	// existing statusMsg handler can merge it into m.status through the
	// normal message channel — i.e., after the event loop has started.
	// It is never read after Init() runs and can otherwise be ignored.
	initStatusMsg tea.Msg

	// zoneFormatter converts the flat status map into structured segments
	// for zone-aware rendering. If nil, a default formatter is used.
	zoneFormatter conduit.StatusFormatter

	// statusLabels maps metadata keys to display labels for the status bar.
	statusLabels map[string]string

	// zonePriorities maps zone names to priority values (lower = higher
	// priority). Zones not in this map get a default priority.
	zonePriorities map[string]int

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

	// theme is the consolidated style configuration for the TUI. It owns
	// the glamour StyleConfig used by the markdown renderer and the
	// lipgloss styles used for chrome (headers, role labels, status
	// line, activity indicator). Task 3 adds the field; later tasks wire
	// it into the renderArtifact role mapping and the TUI constructor.
	theme *theme.Theme

	// name is the application name shown in the terminal window title.
	name string

	// cachedContent holds the last computed viewport content string.
	// It is invalidated by setting contentDirty = true.
	cachedContent string

	// contentDirty is true when cachedContent is stale and needs rebuilding.
	contentDirty bool

	// renderScheduled is true when a debounced render tick is pending.
	renderScheduled bool

	// working indicates a long-running activity (e.g., slash command) is in progress.
	working bool

	// workingDescription is a short label describing the current activity (e.g., "compacting").
	workingDescription string
}

// renderedTurn represents a single turn in the conversation history.
type renderedTurn struct {
	role      state.Role
	blocks    []renderedBlock
	timestamp time.Time
}

// hashToolCallID derives a 4-character truncated hex hash from a toolCallID.
// The hash is deterministic and stateless, making parallel tool calls and
// out-of-order results pairable by visual inspection.
func hashToolCallID(id string) string {
	h := fnv.New32a()
	h.Write([]byte(id))
	return fmt.Sprintf("%04x", h.Sum32())[:4]
}

// renderMarkdown delegates to the model's markdown renderer, falling back
// to a default glamourMarkdownRenderer if none was injected.
func (m *model) renderMarkdown(text string, width int) (string, error) {
	// If no renderer was supplied (e.g. in tests), fall back to the
	// production glamour renderer.
	if m.md == nil {
		m.md = newGlamourMarkdownRenderer(theme.Dark())
	}
	return m.md.Render(text, width)
}

// recalcLayout resizes the viewport to fill the remaining terminal space above
// the horizontal separator, reading the textarea's current height. The textarea
// manages its own height via DynamicHeight.
func (m *model) recalcLayout() {
	if m.height == 0 {
		return
	}

	// Reserve space for one or two separators plus the status bar when
	// status has non-empty content.
	_, statusLines := m.statusLine()
	separatorCount := 1
	if statusLines > 0 {
		separatorCount = 2
	}

	m.viewport.SetHeight(m.height - m.textarea.Height() - separatorCount - statusLines)
	if m.viewport.Height() < 1 {
		m.viewport.SetHeight(1)
	}
}

// statusLine renders the status bar, using zone-aware grouping if a
// formatter is configured, or falling back to the legacy flat format.
func (m *model) statusLine() (string, int) {
	if len(m.status) == 0 {
		return "", 0
	}
	var formatter conduit.StatusFormatter
	if m.zoneFormatter != nil {
		formatter = m.zoneFormatter
	} else {
		// Default: put everything in the "default" zone for backward
		// compatibility (renders without zone brackets).
		formatter = func(status map[string]string) []conduit.StatusSegment {
			var segments []conduit.StatusSegment
			for k, v := range status {
				segments = append(segments, conduit.StatusSegment{
					Label: k,
					Value: v,
					Zone:  "default",
				})
			}
			return segments
		}
	}
	segments := formatter(m.status)
	if m.statusLabels != nil {
		for i := range segments {
			if label, ok := m.statusLabels[segments[i].Label]; ok {
				segments[i].Label = label
			}
		}
	}
	return buildStatusLineFromSegments(m.theme, segments, m.zonePriorities, m.width)
}

// syncViewport rebuilds the cached content string if stale and pushes it to
// the viewport. Call this from Update() after any mutation that affects
// visual output.
func (m *model) syncViewport() {
	m.viewport.SetContent(m.buildContent())
}

// renderArtifact converts an artifact into a renderedBlock. Markdown rendering is
// applied unconditionally for all text-bearing block kinds so the pipeline is role-agnostic.
func (m *model) renderArtifact(art artifact.Artifact, role state.Role) renderedBlock {
	switch a := art.(type) {
	case artifact.Text:
		block := renderedBlock{kind: "text", source: a.Content}
		switch role {
		case state.RoleAssistant:
			block.title = "Assistant"
			block.style = m.theme.AssistantStyle
		case state.RoleUser:
			block.title = "You"
			block.style = m.theme.UserStyle
		case state.RoleTool:
			block.title = "Tool"
			block.style = m.theme.ToolResultStyle
		case state.RoleSystem:
			block.title = "System"
			block.style = m.theme.SystemStyle
		}
		block.expandedByDefault = true
		rendered, err := m.renderMarkdown(a.Content, m.viewport.Width())
		if err == nil {
			block.rendered = rendered
		}
		return block
	case artifact.Reasoning:
		block := renderedBlock{
			kind:              "reasoning",
			source:            a.Content,
			title:             "Thinking",
			style:             m.theme.ThinkingStyle,
			expandedByDefault: false,
		}
		rendered, err := m.renderMarkdown(a.Content, m.viewport.Width())
		if err == nil {
			block.rendered = rendered
		}
		return block
	case artifact.ToolCall:
		source := a.MarkdownString()
		// When no custom display hint is present, MarkdownString() falls back to
		// raw Arguments. Preserve the legacy "Calling:" prefix for readability.
		if source == a.Arguments {
			source = fmt.Sprintf("Calling: %s(%s)", a.Name, a.Arguments)
		}
		compact := compactToolCall(a, m.viewport.Width())
		block := renderedBlock{
			kind:              "tool_call",
			source:            source,
			compact:           compact,
			toolCallID:        a.ID,
			title:             fmt.Sprintf("Assistant · Call %s (%s)", a.Name, hashToolCallID(a.ID)),
			style:             m.theme.AssistantStyle,
			expandedByDefault: false,
		}
		rendered, err := m.renderMarkdown(source, m.viewport.Width())
		if err == nil {
			block.rendered = rendered
		}
		return block
	case artifact.ToolResult:
		source := a.MarkdownString()
		if a.IsError {
			source = "Error: " + source
		}
		block := renderedBlock{
			kind:              "tool_result",
			source:            source,
			toolCallID:        a.ToolCallID,
			title:             fmt.Sprintf("Tool Result (%s)", hashToolCallID(a.ToolCallID)),
			style:             m.theme.ToolResultStyle,
			expandedByDefault: false,
		}
		rendered, err := m.renderMarkdown(source, m.viewport.Width())
		if err == nil {
			block.rendered = rendered
		}
		// Derive compact from rendered Markdown output, falling back to raw
		// source when rendering is unavailable (e.g. user turns or errors).
		if block.rendered != "" {
			block.compact = compactToolResult(block.rendered, m.viewport.Width())
		} else {
			block.compact = compactToolResult(source, m.viewport.Width())
		}
		if a.IsError {
			block.style = m.theme.ErrorStyle
		}
		return block
	case artifact.Compaction:
		return m.renderCompactionBlock(a)
	}
	return renderedBlock{}
}

// renderCompactionBlock produces a one-line marker block for an
// artifact.Compaction. The marker is collapsed by default (just the
// turn count and bytes saved); expanding via Ctrl+O reveals the
// strategy, model, and timestamp as a small metadata footer.
//
// The compaction turn also carries an artifact.Text sibling with the
// LLM-facing summary. That sibling is rendered separately by the
// caller (loadHistory / TURN event handlers iterate per-artifact);
// this function is only responsible for the Compaction artifact
// itself.
func (m *model) renderCompactionBlock(c artifact.Compaction) renderedBlock {
	bytesSaved := formatByteCount(c.DroppedTokenEstimate)
	title := fmt.Sprintf("Compacted %d turns (~%s saved)", c.DroppedTurnCount, bytesSaved)
	marker := fmt.Sprintf("↳ compacted %d turns (~%s saved)", c.DroppedTurnCount, bytesSaved)

	source := fmt.Sprintf(
		"strategy: %s\nmodel: %s\ncompacted through turn %d\nsaved ~%s\nat: %s",
		orDefault(c.Strategy, "(unknown)"),
		orDefault(c.Model, "(default)"),
		c.CompactedThrough,
		bytesSaved,
		c.CreatedAt.Format("15:04:05"),
	)

	return renderedBlock{
		kind:              "compaction",
		source:            source,
		compact:           marker,
		title:             title,
		style:             m.theme.SystemStyle,
		expandedByDefault: false,
	}
}

// formatByteCount formats a byte count into a short human-readable
// form (e.g. 1234 → "1.2K", 1234567 → "1.2M"). Used by the compaction
// marker to keep titles compact.
func formatByteCount(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
}

// orDefault returns s if non-empty, otherwise fallback.
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// renderPlainBlock constructs a renderedBlock for plain text (e.g. error or
// feedback) and runs it through the same Markdown + header pipeline so it
// participates in the unified rendering path.
func (m *model) renderPlainBlock(kind, source, title string, style lipgloss.Style) renderedBlock {
	block := renderedBlock{
		kind:              kind,
		source:            source,
		title:             title,
		style:             style,
		expandedByDefault: true,
	}
	rendered, err := m.renderMarkdown(source, m.viewport.Width())
	if err == nil {
		block.rendered = rendered
	}
	return block
}

// loadHistory pre-populates the model's turn slice from a stream's
// historical conversation state. It is called once during TUI startup
// when resuming an existing thread. The supplied turns are expected to be
// read-only; the method does not modify the slice or its contained artifacts.
func (m *model) loadHistory(turns []state.Turn) {
	for _, turn := range turns {
		var blocks []renderedBlock
		for _, art := range turn.Artifacts {
			block := m.renderArtifact(art, turn.Role)
			if block.kind != "" {
				blocks = append(blocks, block)
			}
		}
		m.turns = append(m.turns, renderedTurn{
			role:      turn.Role,
			blocks:    blocks,
			timestamp: turn.Timestamp,
		})
	}
	if len(m.turns) > 0 {
		m.contentDirty = true
	}
}

// Init returns an initial command. No periodic ticks are needed because
// turns arrive via program.Send from the orchestrator goroutine.
//
// When initModel populated m.initStatusMsg, Init yields it as a tea.Cmd
// so the statusMsg handler picks it up through the normal message
// channel after the event loop is running. This is the only safe place
// to dispatch a message into the program: calling program.Send from
// before p.Run() blocks the main goroutine indefinitely.
func (m *model) Init() tea.Cmd {
	if m.initStatusMsg == nil {
		return nil
	}
	seed := m.initStatusMsg
	return func() tea.Msg { return seed }
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
				m.currentTurn.blocks = append(m.currentTurn.blocks, renderedBlock{kind: "text", source: a.Content, title: "Assistant", style: m.theme.AssistantStyle, expandedByDefault: true})
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
				m.currentTurn.blocks = append(m.currentTurn.blocks, renderedBlock{kind: "reasoning", source: a.Content, title: "Thinking", style: m.theme.ThinkingStyle, expandedByDefault: false})
			}
		default:
			block := m.renderArtifact(msg.artifact, state.RoleAssistant)
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
				if isRerenderableKind(block.kind) && block.source != "" {
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
			m.currentTurn.timestamp = msg.turn.Timestamp
			m.turns = append(m.turns, m.currentTurn)
			m.currentTurn = renderedTurn{}
		} else {
			// User and tool turns do not emit individual ArtifactEvents;
			// build the turn from the full Turn content.
			var blocks []renderedBlock
			for _, art := range msg.turn.Artifacts {
				block := m.renderArtifact(art, msg.turn.Role)
				if block.kind != "" {
					blocks = append(blocks, block)
				}
			}
			rt := renderedTurn{
				role:      msg.turn.Role,
				blocks:    blocks,
				timestamp: msg.turn.Timestamp,
			}
			m.turns = append(m.turns, rt)
		}
		wasAtBottom := m.viewport.AtBottom()
		m.contentDirty = true
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
	case reloadHistoryMsg:
		m.turns = nil
		m.currentTurn = renderedTurn{} // defensive: clear any partial turn
		m.pending = false              // defensive: reset pending state
		m.renderScheduled = false      // clear any pending render tick
		m.loadHistory(msg.turns)
		m.contentDirty = true
		wasAtBottom := m.viewport.AtBottom()
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
	case statusMsg:
		wasAtBottom := m.viewport.AtBottom()
		if m.status == nil {
			m.status = make(map[string]string)
		}
		for k, v := range msg.status {
			m.status[k] = v
		}
		m.recalcLayout()
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
	case lifecycleMsg:
		wasAtBottom := m.viewport.AtBottom()
		switch msg.phase {
		case "submitted":
			m.pending = true
		case "cancelled":
			m.currentTurn = renderedTurn{}
			m.pending = false
			m.renderScheduled = false
		case "done":
			m.pending = false
		}
		if m.status == nil {
			m.status = make(map[string]string)
		}
		m.status["phase"] = msg.phase
		m.contentDirty = true
		m.recalcLayout()
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
	case activityMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.working = msg.active
		if msg.active {
			m.workingDescription = msg.description
		} else {
			m.workingDescription = ""
		}
		m.contentDirty = true
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
	case clearPendingMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.pending = false
		m.contentDirty = true
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
	case audioMsg:
		// Emit the terminal bell as an audible cue. The bell is the
		// same for done and error because \a cannot vary pitch.
		fmt.Print("\a")
		return m, nil
	case errorMsg:
		wasAtBottom := m.viewport.AtBottom()
		// Surface the error to the user and reset UI state so the
		// input area is usable again. Discard any partial assistant
		// output accumulated in currentTurn.
		m.currentTurn = renderedTurn{}
		m.pending = false
		m.renderScheduled = false
		if m.status == nil {
			m.status = make(map[string]string)
		}
		m.status["phase"] = "error"
		block := m.renderPlainBlock("error", msg.err.Error(), "System", m.theme.ErrorStyle)
		m.turns = append(m.turns, renderedTurn{
			role:   state.RoleSystem,
			blocks: []renderedBlock{block},
		})
		m.contentDirty = true
		m.recalcLayout()
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
		return m, nil
	case noticeMsg:
		wasAtBottom := m.viewport.AtBottom()
		block := m.renderPlainBlock("notice", msg.notice.Content, "System", m.theme.StyleForSeverity(msg.notice.Severity))
		m.turns = append(m.turns, renderedTurn{
			role:   state.RoleSystem,
			blocks: []renderedBlock{block},
		})
		m.contentDirty = true
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
		return m, nil
	case renderTickMsg:
		if !m.renderScheduled {
			return m, nil
		}
		for j := range m.currentTurn.blocks {
			block := &m.currentTurn.blocks[j]
			if isRerenderableKind(block.kind) && block.source != "" {
				rendered, err := m.renderMarkdown(block.source, m.viewport.Width())
				if err == nil {
					block.rendered = rendered
				}
			}
		}
		wasAtBottom := m.viewport.AtBottom()
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
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
				wasAtBottom := m.viewport.AtBottom()
				if m.textarea.Value() != "" {
					content := m.textarea.Value()
					m.textarea.Reset()
					m.recalcLayout()
					select {
					case m.eventsCh <- session.UserMessageEvent{Content: content}:
					default:
						slog.Warn("event channel full, dropping user message")
					}
					m.contentDirty = true
					m.syncViewport()
				}
				if wasAtBottom {
					m.viewport.GotoBottom()
				}
				return m, nil
			}
			// Shift+Enter: pass to textarea for newline insertion.
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.recalcLayout()
			return m, cmd
		case tea.KeyEscape:
			select {
			case m.eventsCh <- session.InterruptEvent{}:
			default:
			}
			return m, nil
		}

		// Ctrl+C
		if msg.Key().Code == 'c' && msg.Key().Mod.Contains(tea.ModCtrl) {
			select {
			case m.eventsCh <- session.InterruptEvent{}:
			default:
			}
			return m, tea.Quit
		}

		// Ctrl+J — alternative newline insertion for terminals where Shift+Enter
		// is not available or not passed through.
		if msg.Key().Code == 'j' && msg.Key().Mod.Contains(tea.ModCtrl) {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.recalcLayout()
			return m, cmd
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
			m.expandAllDetails = !m.expandAllDetails
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
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case tea.PasteMsg:
		wasAtBottom := m.viewport.AtBottom()
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.recalcLayout()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
		return m, cmd
	case tea.WindowSizeMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(msg.Width)
		m.textarea.SetWidth(msg.Width)
		m.textarea.MaxHeight = max(3, msg.Height/3)
		m.recalcLayout()
		// Re-render assistant turn blocks with the new terminal width
		// so cached Markdown output remains correctly wrapped.
		for i, turn := range m.turns {
			if turn.role == state.RoleAssistant {
				for j, block := range turn.blocks {
					if isRerenderableKind(block.kind) && block.source != "" {
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
			if isRerenderableKind(block.kind) && block.source != "" {
				rendered, err := m.renderMarkdown(block.source, m.viewport.Width())
				if err == nil {
					m.currentTurn.blocks[j].rendered = rendered
				}
			}
		}
		m.renderScheduled = false
		m.contentDirty = true
		m.syncViewport()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
	}
	return m, nil
}
