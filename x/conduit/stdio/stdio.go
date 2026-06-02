package stdio

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/conduit"
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
	mgr      *session.Manager
	in       io.Reader
	out      io.Writer
	threadID string
}

// Option configures the stdio conduit.
type Option func(*stdio)

// WithInput sets the input reader. Defaults to os.Stdin.
func WithInput(r io.Reader) Option {
	return func(s *stdio) {
		s.in = r
	}
}

// WithOutput sets the output writer. Defaults to os.Stdout.
func WithOutput(w io.Writer) Option {
	return func(s *stdio) {
		s.out = w
	}
}

// WithThreadID sets the thread ID to resume on start.
func WithThreadID(id string) Option {
	return func(s *stdio) {
		s.threadID = id
	}
}

// New creates a new stdio conduit that implements conduit.Conduit.
func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error) {
	if mgr == nil {
		return nil, fmt.Errorf("session manager is required")
	}
	s := &stdio{
		mgr: mgr,
		in:  os.Stdin,
		out: os.Stdout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Start reads input, processes one turn, streams assistant artifacts as Markdown
// blocks to the configured io.Writer, and returns. This is a deliberate
// exception to the standard conduit blocking-contract; the conduit is designed
// for single-shot Unix-filter usage rather than long-running ambient I/O.
func (s *stdio) Start(ctx context.Context) error {
	var stream *session.Stream
	var err error
	if s.threadID != "" {
		stream, err = s.mgr.Attach(s.threadID)
	} else {
		stream, err = s.mgr.Create()
	}
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}

	outputCh := stream.Subscribe("text_delta", "reasoning_delta", "tool_call_delta", "tool_call", "turn_complete", "error", "lifecycle")

	done := make(chan struct{})
	stop := make(chan struct{})
	var turnErr error

	go func() {
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
						// Complete tool_call after accumulation; prefer display string.
						fmt.Fprintf(s.out, "```tool-call\n%s\n```\n", art.MarkdownString())
					}

				case loop.TurnCompleteEvent:
					if e.Turn.Role != state.RoleAssistant {
						continue
					}
					if currentKind == "reasoning_delta" || currentKind == "tool_call_delta" {
						fmt.Fprint(s.out, "\n```\n")
					}
					currentKind = ""

				case loop.LifecycleEvent:
				// Print phase transitions for user feedback.
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
					turnErr = e.Err
					fmt.Fprintf(s.out, "\nerror: %v\n", e.Err)
					return
				}
			case <-stop:
				if currentKind == "reasoning_delta" || currentKind == "tool_call_delta" {
					fmt.Fprint(s.out, "\n```\n")
				}
				return
			}
		}
	}()

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

	event := session.UserMessageEvent{
		Content: string(data),
		Ctx:     loop.WithProvenance(context.Background(), "stdio"),
	}
	processErr := stream.Process(ctx, event)

	// Close the stream to signal completion and let the goroutine drain
	// any remaining events (including ErrorEvent on failure) before the
	// subscriber channel is closed.
	_ = stream.Close()

	select {
	case <-done:
	case <-ctx.Done():
		close(stop)
		<-done
		return ctx.Err()
	}

	if processErr != nil {
		return fmt.Errorf("process event: %w", processErr)
	}

	return turnErr
}
