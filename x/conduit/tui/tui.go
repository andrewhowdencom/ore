// Package tui implements an opinionated terminal user interface conduit for
// the ore framework using Bubble Tea.
//
// Use New(sess, opts...) to create a TUI that wraps an already-attached
// session.Session. The TUI subscribes to the session's output events and
// routes them into the Bubble Tea program; outbound user actions (typed
// messages, interrupts) are produced on a channel returned by Events() for
// the application to consume via session.Runner.Run.
//
// The TUI is a dumb pipe: it does not invoke the provider, does not own the
// session's lifecycle, and does not manage the turn loop. The application is
// responsible for constructing the session, seeding any default metadata
// before Start, and pumping Events() into a session.Runner.
//
// Cancellation is wired through WithCancelFunc: the registered cancel
// function is invoked when the user presses Ctrl+C or Esc inside the TUI.
// The application typically pairs this with a context.WithCancel whose
// cancel func is shared with both Start(ctx) and runner.Run, so a single
// signal unwinds the UI, any in-flight turn, and the runner pump.
//
// Streaming model:
// The TUI subscribes to delta artifact events (text_delta, reasoning_delta,
// tool_call, tool_result, turn_complete) and renders assistant output
// incrementally as chunks arrive. A 16ms debounced render tick batches
// glamour markdown re-renders to keep the UI smooth at ~60fps.
//
// State refresh:
// If the underlying conversation state is replaced (e.g. after compaction via
// thread.Replace), call ReloadHistory on the TUI to rebuild the conversation
// view from the new turn slice. This must be done after Start has been called
// so the Bubble Tea program is running.
//
// Keyboard shortcuts:
//
//	Ctrl+O — toggle expansion of latest assistant turn's tool blocks
//	         (compact by default; resets after each new turn)
//	Ctrl+C — quit
//	Shift+Enter — insert newline in the input box
package tui

import (
	"context"
	"fmt"
	"log/slog"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/compaction"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/andrewhowdencom/ore/x/conduit/tui/theme"
	"go.opentelemetry.io/otel/trace"
)

// readBoundaryFromSession fetches the current compaction BoundaryInfo
// from the session's metadata, returning the zero value if no boundary
// has been recorded. The TUI uses this to render the collapse marker at
// startup; the value is purely advisory (the rendered marker has no
// semantic effect on the conversation).
func readBoundaryFromSession(sess *session.Session) compaction.BoundaryInfo {
	if sess == nil {
		return compaction.BoundaryInfo{}
	}
	encoded, ok := sess.GetMetadata(compaction.MetaKeyBoundaryInfo)
	if !ok {
		return compaction.BoundaryInfo{}
	}
	info, err := compaction.DecodeBoundaryInfo(encoded)
	if err != nil {
		// Treat a malformed boundary as "no boundary"; the TUI
		// should not surface a decode error in the conversation view.
		return compaction.BoundaryInfo{}
	}
	return info
}

// TUI is a terminal user interface conduit. It hides all Bubble Tea internals
// from callers.
type TUI struct {
	sess           *session.Session
	events         chan session.Event
	cancelFunc     context.CancelFunc
	program        *tea.Program
	programOpts    []tea.ProgramOption
	name           string
	zoneFormatter  conduit.StatusFormatter
	zonePriorities map[string]int
	statusLabels   map[string]string
	tracer         trace.Tracer
	theme          *theme.Theme
}

// Option configures a TUI.
type Option func(*TUI)

// WithName sets the application name displayed in the terminal window title.
func WithName(name string) Option {
	return func(t *TUI) {
		t.name = name
	}
}

// WithCancelFunc registers a context.CancelFunc to be invoked when the user
// presses Ctrl+C or Esc inside the TUI. The application typically passes the
// cancel func of a context.WithCancel whose parent ctx is also passed to
// tui.Start and session.Runner.Run, so a single cancel unwinds the UI,
// any in-flight turn, and the runner pump.
func WithCancelFunc(cancel context.CancelFunc) Option {
	return func(t *TUI) {
		t.cancelFunc = cancel
	}
}

// WithStatusZones configures the TUI to group status metadata into
// semantic zones for priority-based rendering. Unmapped keys fall into
// the "default" zone. Lifecycle zones render first, then context.
func WithStatusZones(mapping map[string]string) Option {
	return func(t *TUI) {
		t.zoneFormatter = func(status map[string]string) []conduit.StatusSegment {
			var segments []conduit.StatusSegment
			for k, v := range status {
				zone := mapping[k]
				if zone == "" {
					zone = "default"
				}
				segments = append(segments, conduit.StatusSegment{
					Label: k,
					Value: v,
					Zone:  zone,
				})
			}
			return segments
		}
		t.zonePriorities = map[string]int{
			"lifecycle": 0,
			"context":   1,
			"default":   99,
		}
	}
}

// WithStatusLabels maps metadata keys to display labels in the status bar.
// When a key is present in the mapping, the specified label is rendered in
// place of the raw key name. This is useful for shortening or prettifying
// long or namespaced keys (e.g. "workshop.role" → "role").
func WithStatusLabels(mapping map[string]string) Option {
	return func(t *TUI) {
		t.statusLabels = mapping
	}
}

// WithTracer configures an OpenTelemetry tracer for the TUI.
// When configured, user input events start a "tui.turn" server span
// that is propagated through the event stream for downstream linking.
func WithTracer(tracer trace.Tracer) Option {
	return func(t *TUI) {
		t.tracer = tracer
	}
}

// WithTheme configures a custom theme for the TUI. When omitted, the
// TUI calls theme.Auto() at startup to select dark or light based on
// the terminal's reported background. Pass a theme from the
// x/conduit/tui/theme package (e.g. theme.Dark(), theme.Light(), or
// a custom *theme.Theme) to override the default.
func WithTheme(th *theme.Theme) Option {
	return func(t *TUI) {
		t.theme = th
	}
}

// WithProgramOptions appends Bubble Tea ProgramOption values that are
// applied when Start constructs the underlying tea.Program. This is
// primarily intended for tests that need to run the program in a
// non-interactive environment (e.g. tea.WithoutRenderer,
// tea.WithoutSignals). Calling this option multiple times accumulates
// the supplied options in call order. Pass no arguments to clear any
// previously-supplied options.
func WithProgramOptions(opts ...tea.ProgramOption) Option {
	return func(t *TUI) {
		t.programOpts = append(t.programOpts, opts...)
	}
}

// themeOrAuto returns the configured theme or, if none was supplied via
// WithTheme, the result of theme.Auto() for the current terminal. It
// caches the auto-detected theme on the TUI so the renderer and model
// agree on the same instance.
func (t *TUI) themeOrAuto() *theme.Theme {
	if t.theme == nil {
		t.theme = theme.Auto()
	}
	return t.theme
}

// Descriptor enumerates the capabilities of the TUI conduit.
// CapAudioNotification is included because the TUI satisfies the
// AudioNotifier contract using the terminal bell (\a), the only
// universally-available sound in terminal environments.
var Descriptor = conduit.Descriptor{
	Name:        "TUI",
	Description: "Terminal user interface via Bubble Tea",
	Capabilities: []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapShowStatus,
		conduit.CapRenderTurn,
		conduit.CapRenderMarkdown,
		conduit.CapAudioNotification,
	},
}

// Compile-time assertion that *TUI implements conduit.AudioNotifier.
var _ conduit.AudioNotifier = (*TUI)(nil)

// New creates a new TUI conduit that wraps the given session. The
// returned value must be started with Start(ctx) to run the interface.
// The session must not be nil; the TUI reads from it (turns, metadata,
// Subscribe) but does not own its lifecycle. The application is
// responsible for attaching or creating the session before calling New
// and for pumping Events() into a session.Runner.
//
// Available options include WithName, WithCancelFunc, WithTracer,
// WithTheme, WithStatusZones, WithStatusLabels, and WithProgramOptions.
func New(sess *session.Session, opts ...Option) (conduit.Conduit, error) {
	if sess == nil {
		return nil, fmt.Errorf("session is required")
	}
	t := &TUI{sess: sess, name: "Ore"}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// Events returns a buffered channel of user-initiated session events.
// The application is expected to consume this channel and pass each event
// to session.Runner.Run(ctx, sess, evt). The channel is created lazily on
// the first call to Start and is closed when Start returns.
//
// Events produced on the channel include session.UserMessageEvent when
// the user presses Enter and session.InterruptEvent when the user
// presses Ctrl+C or Esc. Per-event provenance is attached via
// loop.WithProvenance on the event's Ctx field.
func (t *TUI) Events() <-chan session.Event {
	return t.events
}

// initModel creates and initializes the Bubble Tea model for the TUI,
// including pre-populating historical turns from the session when resuming
// an existing conversation.
//
// ctx is the runtime context attached to every emitted session.Event via
// loop.WithProvenance. cancelFunc, if non-nil, is invoked by the model on
// Ctrl+C and Esc after the corresponding session.InterruptEvent is emitted
// on eventsCh; the application uses this to cancel its runner pump and
// unwind any in-flight turn.
func (t *TUI) initModel(ctx context.Context, eventsCh chan session.Event, sess *session.Session) model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.Focus()

	m := model{
		eventsCh:       eventsCh,
		ctx:            ctx,
		cancelFunc:     t.cancelFunc,
		viewport:       viewport.New(),
		textarea:       ta,
		md:             newGlamourMarkdownRenderer(t.themeOrAuto()),
		theme:          t.themeOrAuto(),
		name:           t.name,
		zoneFormatter:  t.zoneFormatter,
		statusLabels:   t.statusLabels,
		zonePriorities: t.zonePriorities,
	}

	// Pre-populate the model with historical turns from the session.
	// The boundary info is read from session metadata; loadHistory
	// renders the collapse marker if one is present.
	m.loadHistory(sess.Turns(), readBoundaryFromSession(sess))

	// Resolve the status-bar seed up front. It is delivered via Init()'s
	// tea.Cmd (not via a direct Send) so the message reaches the
	// statusMsg handler through the normal channel after the event loop
	// has started. statusFromSession returns nil if there is no
	// metadata, which Init() also treats as a no-op.
	m.initStatusMsg = statusFromSession(sess)
	return m
}

// statusFromSession returns the statusMsg that should be sent to the
// program on Start, seeded from the session's current metadata. Returns
// nil if the session has no metadata, so the caller can skip a no-op
// Send.
//
// The bootstrap exists because applications typically seed default
// metadata (thread_id, cwd, git_branch, etc.) on the session before
// constructing the TUI. Those writes do not produce a PropertiesEvent
// that reaches the TUI's Subscribe stream (which is live-only), so the
// status bar would otherwise render empty on the first frame. Sending
// a single statusMsg before the live-event goroutine starts keeps
// status updates funneled through the existing statusMsg handler (a
// merge, not a replace) so a concurrent live PropertiesEvent for the
// same key is a no-op.
func statusFromSession(sess *session.Session) tea.Msg {
	if meta := sess.AllMetadata(); len(meta) > 0 {
		return statusMsg{status: meta}
	}
	return nil
}

// Start initializes the Bubble Tea program, subscribes to the session
// output stream, and blocks until the user quits (Ctrl+C) or ctx is
// cancelled. On context cancellation the program exits gracefully and
// the Events() channel is closed.
//
// Start is a no-op turn-loop driver: it neither invokes the provider
// nor manages the inference pipeline. The application is expected to
// range over Events() in a separate goroutine and pass each event to
// session.Runner.Run.
func (t *TUI) Start(ctx context.Context) error {
	surfEventsCh := make(chan session.Event, 16)
	m := t.initModel(ctx, surfEventsCh, t.sess)
	p := tea.NewProgram(&m, t.programOpts...)
	t.events = surfEventsCh
	t.program = p

	// Subscribe to the session's output, including delta artifact kinds
	// so the TUI can accumulate assistant content incrementally as
	// each delta chunk arrives, rather than waiting for
	// TurnCompleteEvent.
	outputCh := t.sess.Subscribe(
		"text_delta", "reasoning_delta", "tool_call", "tool_result",
		"turn_complete", "error", "properties", "lifecycle", "notice", "activity",
	)

	// Goroutine to stream output events into the Bubble Tea message loop.
	go func() {
		for event := range outputCh {
			switch e := event.(type) {
			case loop.ArtifactEvent:
				p.Send(artifactMsg{artifact: e.Artifact})
			case loop.TurnCompleteEvent:
				p.Send(turnMsg{turn: e.Turn})
			case loop.ErrorEvent:
				p.Send(errorMsg{err: e.Err})
				_ = t.PlayError(ctx)
			case loop.LifecycleEvent:
				p.Send(lifecycleMsg{phase: e.Phase})
				if e.Phase == "done" {
					_ = t.PlayDone(ctx)
				}
			case loop.PropertiesEvent:
				// Convert the operation-tagged event into a statusMsg
				// carrying both sets and deletes. Set ops are folded
				// into the status map; delete ops populate the
				// deletions slice. Order is preserved on the
				// receiving side so a mixed batch in one event is
				// applied in the same order it was emitted.
				sets := make(map[string]string)
				var dels []string
				for _, op := range e.Operations {
					switch op.Op {
					case loop.PropertyOpSet:
						sets[op.Key] = op.Value
					case loop.PropertyOpDelete:
						dels = append(dels, op.Key)
					}
				}
				p.Send(statusMsg{status: sets, deletions: dels})
			case loop.ActivityEvent:
				p.Send(activityMsg{active: e.Active, description: e.Description})
			case loop.NoticeEvent:
				p.Send(noticeMsg{notice: e.Notice})
			}
		}
	}()

	// Goroutine to quit the program when the context is cancelled.
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	_, err := p.Run()
	close(t.events)
	if err != nil {
		slog.Debug("tui: program returned error", "err", err)
	}
	return err
}

// PlayDone forwards an audio notification to the Bubble Tea model so the
// terminal bell is emitted on the UI goroutine. This preserves the
// single-threaded model and avoids race conditions with the event loop.
// The TUI uses the same bell for both done and error because \a cannot
// vary pitch; distinct tones require a richer audio backend.
func (t *TUI) PlayDone(ctx context.Context) error {
	t.program.Send(audioMsg{})
	return nil
}

// PlayError forwards an audio notification to the Bubble Tea model so the
// terminal bell is emitted on the UI goroutine. Because the terminal
// bell (\a) cannot vary pitch, the TUI produces the same sound for
// errors as for successful turns. A future richer audio backend could introduce
// distinct error tones.
func (t *TUI) PlayError(ctx context.Context) error {
	t.program.Send(audioMsg{})
	return nil
}

// ReloadHistory discards the model's current conversation history and
// rebuilds it from the supplied turn slice. This is the public hook that
// downstream applications (e.g., a slash command handler or compaction
// processor) call after replacing the session's persistent state via
// thread.Replace so the TUI view stays synchronized with the backend.
//
// boundary is the BoundaryInfo for the latest compaction, if any.
// Pass the zero value when no compaction has occurred; the TUI
// renders no collapse marker in that case.
func (t *TUI) ReloadHistory(turns []ledger.Turn, boundary compaction.BoundaryInfo) error {
	if t.program != nil {
		t.program.Send(reloadHistoryMsg{turns: turns, boundary: boundary})
	}
	return nil
}

// invokeCancelFunc invokes the registered WithCancelFunc function, if any.
// Used by the model on Ctrl+C and Esc to ensure the cancel signal reaches
// the application even if the Events() channel has not yet been read by
// the runner pump.
func (t *TUI) invokeCancelFunc() {
	if t.cancelFunc != nil {
		t.cancelFunc()
	}
}