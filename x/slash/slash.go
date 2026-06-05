package slash

import (
	"context"
	"strings"
	"sync"

	"github.com/andrewhowdencom/ore/session"
)

// Handler is a slash command handler. It receives the parsed command arguments
// and returns an error if the command fails.
// A nil error with a nil event means the original event is consumed (no LLM processing).
// A nil error with a non-nil event means the event is replaced with the returned one.
type Handler func(ctx context.Context, args []string) (session.Event, error)

// Registry is the slash command registry interface.
type Registry interface {
	Bind(name string, handler Handler)
	Intercept(ctx context.Context, event session.Event) (session.Event, bool, error)
}

// Compile-time assertion that registry implements session.Interceptor.
var _ session.Interceptor = (*registry)(nil)

type registry struct {
	mu       sync.RWMutex
	commands map[string]Handler
}

// NewRegistry creates a new empty slash command registry.
func NewRegistry() Registry {
	return &registry{
		commands: make(map[string]Handler),
	}
}

// Bind registers a handler for a slash command. The name should not include
// the leading "/"; e.g., Bind("new", handler) matches "/new" in user messages.
func (r *registry) Bind(name string, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[name] = handler
}

// Intercept implements session.Interceptor. It inspects UserMessageEvent content
// for leading slash commands. If a matching command is found, the handler is
// invoked and the event is consumed (returned bool is true). If no command
// matches, the event passes through unchanged (returned bool is false).
// Non-UserMessageEvent types are always passed through unchanged.
func (r *registry) Intercept(ctx context.Context, event session.Event) (session.Event, bool, error) {
	ume, ok := event.(session.UserMessageEvent)
	if !ok {
		return event, false, nil
	}

	parts := strings.Fields(ume.Content)
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "/") {
		return event, false, nil
	}

	command := strings.TrimPrefix(parts[0], "/")

	r.mu.RLock()
	handler, ok := r.commands[command]
	r.mu.RUnlock()

	if !ok {
		return event, false, nil
	}

	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}

	newEvent, err := handler(ctx, args)
	if err != nil {
		return event, false, err
	}
	if newEvent == nil {
		return event, true, nil
	}
	return newEvent, false, nil
}
