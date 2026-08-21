package stdio

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/conduit"
	"go.opentelemetry.io/otel/trace"
)

// Descriptor enumerates the capabilities of the stdio conduit.
var Descriptor = conduit.Descriptor{
	Name:        "stdio",
	Description: "Single-shot stdin/stdout/file I/O conduit",
	Capabilities: []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapRenderMarkdown,
		conduit.CapAcceptText,
	},
}

type stdio struct {
	sess   *session.Session
	in     io.Reader
	out    io.Writer
	err    io.Writer
	tracer trace.Tracer
}

// Option configures the stdio conduit.
type Option func(*stdio)

// WithInput sets the input reader. Defaults to os.Stdin.
func WithInput(r io.Reader) Option {
	return func(s *stdio) {
		s.in = r
	}
}

// WithOutput sets the stdout-equivalent output writer. Defaults to os.Stdout.
func WithOutput(w io.Writer) Option {
	return func(s *stdio) {
		s.out = w
	}
}

// WithStderr sets the stderr-equivalent output writer. Notice and other
// out-of-band events are written here with a severity label prefix
// (e.g. "Error: role \"foo\" not found"). Defaults to os.Stderr.
func WithStderr(w io.Writer) Option {
	return func(s *stdio) {
		s.err = w
	}
}

// WithTracer configures an OpenTelemetry tracer for the stdio conduit.
func WithTracer(tracer trace.Tracer) Option {
	return func(s *stdio) {
		s.tracer = tracer
	}
}

// New creates a new session-shaped stdio conduit that drives a
// single inference turn on the supplied session. The session is
// expected to be registered with an engine elsewhere; New binds
// the conduit to the session's existing event stream and renders
// output to the configured io.Writer(s).
//
// Thread lookup or hydration is the caller's responsibility — use
// a session.Backend (e.g. the httpc.Backend implementation in
// workshop) to attach to an existing thread ID before passing
// the session in. The legacy WithThreadID option is gone: the
// session already carries the thread identity.
func New(sess *session.Session, opts ...Option) (conduit.Conduit, error) {
	if sess == nil {
		return nil, fmt.Errorf("session is required")
	}
	s := &stdio{
		sess: sess,
		in:   os.Stdin,
		out:  os.Stdout,
		err:  os.Stderr,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Start reads input, submits a single user turn to the bound
// session, and streams the session's events to the configured
// io.Writers. Returns when the turn is complete (lifecycle "done")
// or on error. This is a deliberate exception to the standard
// conduit blocking-contract; the conduit is designed for
// single-shot Unix-filter usage rather than long-running ambient
// I/O.
func (s *stdio) Start(ctx context.Context) error {
	outputCh := s.sess.Subscribe(
		"text_delta", "reasoning_delta", "tool_call", "tool_result",
		"turn_complete", "error", "properties", "lifecycle", "notice", "activity",
	)

	done := make(chan struct{})
	stop := make(chan struct{})
	var turnErr error

	go s.renderLoop(outputCh, &turnErr, done, stop)

	data, err := io.ReadAll(s.in)
	if err != nil {
		close(stop)
		<-done
		return fmt.Errorf("read input: %w", err)
	}
	if len(data) == 0 {
		close(stop)
		<-done
		return fmt.Errorf("no input provided")
	}

	turnCtx := ctx
	var span trace.Span
	if s.tracer != nil {
		turnCtx, span = s.tracer.Start(turnCtx, "stdio.turn", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
	}

	// Submit the user message as a turn. session.Submit appends
	// the turn to the bound thread and emits a TurnCompleteEvent
	// when the engine processes it. The engine (which the caller
	// has wired) drives the actual inference; this stdio just
	// blocks on the subscriber channel until "done".
	if _, err := s.sess.Submit(turnCtx, ledger.RoleUser,
		artifact.Text{Content: string(data)}); err != nil {
		close(stop)
		<-done
		return fmt.Errorf("submit user message: %w", err)
	}

	_ = s.sess.Close()

	select {
	case <-done:
	case <-ctx.Done():
		close(stop)
		<-done
		return ctx.Err()
	}

	if turnErr != nil {
		return fmt.Errorf("turn error: %w", turnErr)
	}
	return nil
}

// renderLoop drains the output channel, rendering assistant
// artifacts, lifecycle transitions, and errors to the configured
// io.Writers. Tool-call deltas are detected and rendered as
// inline JSON; complete tool_call artifacts (delivered after
// the upstream accumulator folds the deltas) are rendered as a
// Markdown code block.
func (s *stdio) renderLoop(outputCh <-chan loop.OutputEvent, turnErr *error, done, stop chan struct{}) {
	defer close(done)
	currentKind := ""
	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				return
			}
			if p, _ := loop.ProvenanceFrom(event.Context()); p != "stdio" && p != "" {
				continue
			}

			switch e := event.(type) {
			case loop.ArtifactEvent:
				kind := e.Artifact.Kind()
				if kind != currentKind {
					if currentKind == "reasoning_delta" || currentKind == "tool_call_delta" {
						fmt.Fprint(s.out, "\n```\n")
					}
					if kind == "reasoning_delta" {
						fmt.Fprint(s.out, "```reasoning\n")
					} else if kind == "tool_call_delta" {
						fmt.Fprint(s.out, "```tool-call\n")
					}
					currentKind = kind
				}

				switch art := e.Artifact.(type) {
				case artifact.TextDelta:
					fmt.Fprint(s.out, art.Content)
				case artifact.ReasoningDelta:
					fmt.Fprint(s.out, art.Content)
				case artifact.ToolCallDelta:
					if art.Name != "" {
						fmt.Fprintf(s.out, "%s: ", art.Name)
					}
					fmt.Fprint(s.out, art.Arguments)
				case artifact.ToolCall:
					fmt.Fprintf(s.out, "```tool-call\n%s\n```\n", art.MarkdownString())
				}

			case loop.TurnCompleteEvent:
				if e.Turn.Role != ledger.RoleAssistant {
					continue
				}
				if currentKind == "reasoning_delta" || currentKind == "tool_call_delta" {
					fmt.Fprint(s.out, "\n```\n")
				}
				currentKind = ""

			case loop.LifecycleEvent:
				switch e.Phase {
				case "submitted":
					fmt.Fprint(s.out, "\n")
				case "done":
					if currentKind == "reasoning_delta" || currentKind == "tool_call_delta" {
						fmt.Fprint(s.out, "\n```\n")
					}
					currentKind = ""
				}

			case loop.ErrorEvent:
				if currentKind == "reasoning_delta" || currentKind == "tool_call_delta" {
					fmt.Fprint(s.out, "\n```\n")
				}
				*turnErr = e.Err
				fmt.Fprintf(s.out, "\nerror: %v\n", e.Err)
				return

			case loop.NoticeEvent:
				fmt.Fprintf(s.err, "%s: %s\n", e.Notice.Severity, e.Notice.Content)
			}
		case <-stop:
			if currentKind == "reasoning_delta" || currentKind == "tool_call_delta" {
				fmt.Fprint(s.out, "\n```\n")
			}
			return
		}
	}
}