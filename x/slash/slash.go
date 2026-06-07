package slash

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
)

// Command represents a parsed slash command invocation.
type Command struct {
	Name  string // e.g. "help" (no leading slash)
	Input string // everything after the command, raw and unmodified
}

// Result is the return value from a slash command handler.
type Result struct {
	Replace  session.Event // nil = consume, non-nil = continue with this event
	Feedback artifact.Text // single ephemeral UI message, not persisted
}

// Handler is a slash command handler. It receives the parsed command and
// returns a Result. A nil Result.Replace with nil error means the event is
// consumed (no LLM processing). A non-nil Result.Replace means the event is
// replaced with the returned one. Result.Feedback emits an ephemeral UI
// message that is not persisted to state.
type Handler func(ctx context.Context, cmd Command) (Result, error)

// Fields is a convenience helper that splits the raw command input on
// whitespace. Callers can bring their own parser (e.g. cobra, shlex) when
// custom argument parsing is needed.
func Fields(input string) []string {
	return strings.Fields(input)
}

// Registry is the slash command registry interface.
type Registry interface {
	Bind(name string, description string, handler Handler)
	Intercept(ctx context.Context, event session.Event) (session.InterceptResult, error)
}

// Compile-time assertion that registry implements session.Interceptor.
var _ session.Interceptor = (*registry)(nil)

type registry struct {
	mu           sync.RWMutex
	commands     map[string]Handler
	descriptions map[string]string
}

// NewRegistry creates a new slash command registry with an auto-registered
// /help command that lists all bound commands and their descriptions.
func NewRegistry() Registry {
	r := &registry{
		commands:     make(map[string]Handler),
		descriptions: make(map[string]string),
	}
	r.Bind("help", "Show available slash commands", func(ctx context.Context, cmd Command) (Result, error) {
		r.mu.RLock()
		defer r.mu.RUnlock()
		var lines []string
		lines = append(lines, "Available commands:")
		names := make([]string, 0, len(r.descriptions))
		for name := range r.descriptions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			lines = append(lines, fmt.Sprintf("  /%s — %s", name, r.descriptions[name]))
		}
		return Result{
			Feedback: artifact.Text{Content: strings.Join(lines, "\n")},
		}, nil
	})
	return r
}

// Bind registers a handler for a slash command. The name should not include
// the leading "/"; e.g., Bind("new", "Create a new session", handler)
// matches "/new" in user messages. The description is used by /help.
func (r *registry) Bind(name string, description string, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands[name] = handler
	r.descriptions[name] = description
}

// Intercept implements session.Interceptor. It inspects UserMessageEvent content
// for leading slash commands. If a matching command is found, the handler is
// invoked and the event is consumed or replaced. If no command matches, the
// event passes through unchanged. Non-UserMessageEvent types are always passed
// through unchanged.
func (r *registry) Intercept(ctx context.Context, event session.Event) (session.InterceptResult, error) {
	ume, ok := event.(session.UserMessageEvent)
	if !ok {
		return session.InterceptResult{Event: event}, nil
	}

	content := ume.Content
	trimmed := strings.TrimLeftFunc(content, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if !strings.HasPrefix(trimmed, "/") {
		return session.InterceptResult{Event: event}, nil
	}

	rest := trimmed[1:]
	var command, input string
	if i := strings.IndexFunc(rest, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }); i >= 0 {
		command = rest[:i]
		input = rest[i+1:]
	} else {
		command = rest
		input = ""
	}

	r.mu.RLock()
	handler, ok := r.commands[command]
	r.mu.RUnlock()

	if !ok {
		// Unknown command — emit feedback without triggering inference.
		return session.InterceptResult{
			Feedback: []artifact.Text{
				{Content: fmt.Sprintf("Unknown command: /%s. Type /help for available commands.", command)},
			},
		}, nil
	}

	result, err := handler(ctx, Command{Name: command, Input: input})
	if err != nil {
		return session.InterceptResult{Event: event}, err
	}

	var interceptResult session.InterceptResult
	interceptResult.Event = result.Replace
	if result.Feedback.Content != "" {
		interceptResult.Feedback = []artifact.Text{result.Feedback}
	}
	return interceptResult, nil
}
