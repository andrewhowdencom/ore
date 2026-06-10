// Package tui implements an opinionated terminal user interface conduit for
// the ore framework using Bubble Tea.
//
// Use New(mgr, opts...) to create a TUI that composes with a session.Manager.
// The TUI creates or attaches to a session on Start, subscribes to the
// session's output stream, and sends user events back through it.
// Available options include WithThreadID to resume an existing thread.
//
// Streaming model:
// The TUI subscribes to delta artifact events (text_delta, reasoning_delta,
// tool_call, tool_result, turn_complete) and renders assistant output
// incrementally as chunks arrive. A 16ms debounced render tick batches
// glamour markdown re-renders to keep the UI smooth at ~60fps.
//
// State refresh:
// If the underlying conversation state is replaced (e.g. after compaction via
// stream.LoadTurns), call ReloadHistory on the TUI to rebuild the conversation
// view from the new turn slice. This must be done after Start has been called
// so the Bubble Tea program is running.
//
// Keyboard shortcuts:
//   Ctrl+O — toggle expansion of latest assistant turn's tool blocks
//            (compact by default; resets after each new turn)
//   Ctrl+C — quit
//   Shift+Enter — insert newline in the input box
package tui

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"go.opentelemetry.io/otel/trace"
)

// TUI is a terminal user interface conduit. It hides all Bubble Tea internals
// from callers.
type TUI struct {
	mgr            *session.Manager
	threadID       string
	eventsCh       chan session.Event
	program        *tea.Program
	name           string
	zoneFormatter  conduit.StatusFormatter
	zonePriorities map[string]int
	statusLabels   map[string]string
	tracer         trace.Tracer
}

// Option configures a TUI.
type Option func(*TUI)

// WithThreadID sets the thread ID to resume when starting the TUI.
// An empty string means create a new session.
func WithThreadID(id string) Option {
	return func(t *TUI) {
		t.threadID = id
	}
}

// WithName sets the application name displayed in the terminal window title.
func WithName(name string) Option {
	return func(t *TUI) {
		t.name = name
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

// New creates a new TUI conduit that implements conduit.Conduit.
// The returned value must be started with Start(ctx) to run the interface.
// Available options: WithThreadID(id) to resume an existing thread.
func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error) {
	if mgr == nil {
		return nil, fmt.Errorf("session manager is required")
	}
	t := &TUI{mgr: mgr, name: "Ore"}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// initModel creates and initializes the Bubble Tea model for the TUI,
// including pre-populating historical turns from the stream when resuming
// an existing thread.
func (t *TUI) initModel(eventsCh chan session.Event, stream *session.Stream) model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.Focus()

	m := model{
		eventsCh:       eventsCh,
		viewport:       viewport.New(),
		textarea:       ta,
		md:             newGlamourMarkdownRenderer(),
		name:           t.name,
		zoneFormatter:  t.zoneFormatter,
		statusLabels:   t.statusLabels,
		zonePriorities: t.zonePriorities,
	}

	// Pre-populate the model with historical turns when resuming a thread.
	m.loadHistory(stream.Turns())
	return m
}

// Start creates or attaches to a session, initializes the Bubble Tea program,
// subscribes to the session output stream, and blocks until the user quits
// (Ctrl+C) or ctx is cancelled. On context cancellation the program exits
// gracefully.
func (t *TUI) Start(ctx context.Context) error {
	var stream *session.Stream
	var err error
	if t.threadID != "" {
		stream, err = t.mgr.Attach(t.threadID)
		if err != nil {
			return fmt.Errorf("attach to thread %q: %w", t.threadID, err)
		}
	} else {
		stream, err = t.mgr.Create()
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		slog.Info("thread started", "id", stream.ID())
	}

	surfEventsCh := make(chan session.Event, 10)
	m := t.initModel(surfEventsCh, stream)
	p := tea.NewProgram(&m)
	t.eventsCh = surfEventsCh
	t.program = p

	// Subscribe to the stream's output, including delta artifact kinds so
	// the TUI can accumulate assistant content incrementally as each delta
	// chunk arrives, rather than waiting for TurnCompleteEvent.
	outputCh := stream.Subscribe("text_delta", "reasoning_delta", "tool_call", "tool_result", "turn_complete", "error", "properties", "lifecycle", "feedback", "activity")

	// Goroutine to stream output events into the Bubble Tea message loop.
	go func() {
		for event := range outputCh {
			switch e := event.(type) {
			case loop.ArtifactEvent:
				t.program.Send(artifactMsg{artifact: e.Artifact})
			case loop.TurnCompleteEvent:
				t.program.Send(turnMsg{turn: e.Turn})
			case loop.ErrorEvent:
				t.program.Send(errorMsg{err: e.Err})
				_ = t.PlayError(ctx)
			case loop.LifecycleEvent:
				t.program.Send(lifecycleMsg{phase: e.Phase})
				if e.Phase == "done" {
					_ = t.PlayDone(ctx)
				}
			case loop.PropertiesEvent:
				t.program.Send(statusMsg{status: e.Properties})
			case loop.ActivityEvent:
				t.program.Send(activityMsg{active: e.Active, description: e.Description})
			case loop.FeedbackEvent:
				t.program.Send(feedbackMsg{content: e.Content})
			}
		}
	}()

	// Goroutine to process user events through the session.
	go func() {
		for event := range t.eventsCh {
			switch e := event.(type) {
			case session.UserMessageEvent:
				ctx := context.Background()
				var span trace.Span
				if t.tracer != nil {
					ctx, span = t.tracer.Start(ctx, "tui.turn", trace.WithSpanKind(trace.SpanKindServer))
				}
				msg := session.UserMessageEvent{
					Content: e.Content,
					Ctx:     loop.WithProvenance(ctx, "tui"),
				}
				if err := stream.Submit(msg); err != nil {
					slog.Error("submit failed", "err", err)
				}
				if span != nil {
					span.End()
				}
			case session.InterruptEvent:
				if err := stream.Cancel(); err != nil {
					slog.Error("cancel failed", "err", err)
				}
			}
		}
	}()

	// Goroutine to quit the program when the context is cancelled.
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	_, err = p.Run()
	close(t.eventsCh)
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
// errors as for successful turns. A future richer backend could introduce
// distinct error tones.
func (t *TUI) PlayError(ctx context.Context) error {
	t.program.Send(audioMsg{})
	return nil
}

// ReloadHistory discards the model's current conversation history and
// rebuilds it from the supplied turn slice. This is the public hook that
// downstream applications (e.g., a slash command handler or compaction
// processor) call after replacing the stream's persistent state via
// stream.LoadTurns so the TUI view stays synchronized with the backend.
func (t *TUI) ReloadHistory(turns []state.Turn) error {
	if t.program != nil {
		t.program.Send(reloadHistoryMsg{turns: turns})
	}
	return nil
}
