package stdio

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/andrewhowdencom/ore/session"
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

// Start reads input, processes one turn, and returns.
func (s *stdio) Start(ctx context.Context) error {
	return nil
}
