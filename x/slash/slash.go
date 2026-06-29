package slash

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/junk"
)

// Command represents a parsed slash command invocation.
type Command struct {
	Name  string // e.g. "help" (no leading slash)
	Input string // everything after the command, raw and unmodified

	// stream is the active junk.Stream that the slash command is
	// running in. Handlers that need to mutate thread-scoped state
	// (e.g. metadata) access it via the Stream() method. It is nil for
	// Command values constructed directly in tests; only the registry's
	// Intercept sets it.
	stream *junk.Stream
}

// Stream returns the junk.Stream that owns the in-flight event the
// slash command was parsed from. It is nil for Command values that were
// not produced by a junk.Interceptor (e.g. hand-constructed in tests).
// Handlers that mutate thread state via SetMetadata must check for nil
// before dereferencing.
func (c Command) Stream() *junk.Stream { return c.stream }

// Result is the return value from a slash command handler.
//
// Notice carries an ephemeral, user-visible message that the slash
// interceptor emits as a loop.NoticeEvent. The Severity lets conduits
// pick a rendering style (Success, Info, Warn, Error). A zero-value
// Notice means "no notice to emit"; an empty Content is also skipped.
type Result struct {
	Replace junk.Event  // nil = consume, non-nil = continue with this event
	Notice  loop.Notice    // single ephemeral UI message, not persisted
}

// Handler is a slash command handler. It receives the parsed command and
// a loop.Emitter for signaling activity. It returns a Result and an error.
//
// Error handling: a non-nil error from a handler is intercepted at the
// registry boundary and converted into a Notice{Severity: SeverityError}
// carrying the error's message. The error is also logged via slog.Debug
// for telemetry consumers. Intercept always returns nil in that case so
// conduits see the failure as a user-visible notice rather than having
// it silently dropped.
//
// If a handler sets Result.Notice and also returns a non-nil error, the
// explicit Notice takes precedence — handlers can customise error
// presentation by populating Notice themselves.
type Handler func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error)

// Fields is a convenience helper that splits the raw command input on
// whitespace. Callers can bring their own parser (e.g. cobra, shlex) when
// custom argument parsing is needed.
func Fields(input string) []string {
	return strings.Fields(input)
}

// Registry is the slash command registry interface.
type Registry interface {
	Bind(name string, description string, handler Handler)
	Intercept(ctx context.Context, event junk.Event, stream *junk.Stream, emitter loop.Emitter) (junk.InterceptResult, error)
}

// Compile-time assertion that registry implements junk.Interceptor.
var _ junk.Interceptor = (*registry)(nil)

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
	r.Bind("help", "Show available slash commands", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
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
			lines = append(lines, fmt.Sprintf("* `/%s` — %s", name, r.descriptions[name]))
		}
		return Result{
			Notice: loop.Notice{
				Content:  strings.Join(lines, "\n"),
				Severity: loop.SeverityInfo,
			},
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

// Intercept implements junk.Interceptor. It inspects UserMessageEvent content
// for leading slash commands. If a matching command is found, the handler is
// invoked and the event is consumed or replaced. If no command matches, the
// event passes through unchanged. Non-UserMessageEvent types are always passed
// through unchanged.
//
// The active *junk.Stream is threaded through to the parsed Command so
// handlers that need to mutate thread state (e.g. via SetMetadata) can
// recover it via Command.Stream().
//
// Error handling: handler errors are auto-converted into
// Notice{Severity: SeverityError} and Intercept returns nil. This replaces
// the previous behaviour of propagating the error downstream where it was
// silently dropped. The error is also logged via slog.Debug so existing
// telemetry consumers that grep slog output continue to see it.
func (r *registry) Intercept(ctx context.Context, event junk.Event, stream *junk.Stream, emitter loop.Emitter) (junk.InterceptResult, error) {
	ume, ok := event.(junk.UserMessageEvent)
	if !ok {
		return junk.InterceptResult{Event: event}, nil
	}

	content := ume.Content
	trimmed := strings.TrimLeftFunc(content, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if !strings.HasPrefix(trimmed, "/") {
		return junk.InterceptResult{Event: event}, nil
	}

	rest := trimmed[1:]
	// Skip leading whitespace after the slash to find the command name.
	start := 0
	for start < len(rest) && (rest[start] == ' ' || rest[start] == '\t' || rest[start] == '\n' || rest[start] == '\r') {
		start++
	}
	rest = rest[start:]

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
		// Unknown command — emit an info-severity notice without triggering inference.
		return junk.InterceptResult{
			Notice: []loop.Notice{{
				Content:  fmt.Sprintf("Unknown command: /%s. Type /help for available commands.", command),
				Severity: loop.SeverityInfo,
			}},
		}, nil
	}

	result, err := handler(ctx, emitter, Command{Name: command, Input: input, stream: stream})
	if err != nil {
		// Auto-convert handler errors into error-severity notices so the
		// user sees the failure as a chat message rather than having it
		// silently dropped downstream. If the handler also set a Notice,
		// prefer that — handlers can customise error presentation.
		slog.Debug("slash handler returned error", "command", command, "err", err)

		var notices []loop.Notice
		if result.Notice.Content != "" {
			notices = append(notices, result.Notice)
		} else {
			notices = append(notices, loop.Notice{
				Content:  err.Error(),
				Severity: loop.SeverityError,
			})
		}
		return junk.InterceptResult{Notice: notices}, nil
	}

	var interceptResult junk.InterceptResult
	interceptResult.Event = result.Replace
	if result.Notice.Content != "" {
		interceptResult.Notice = []loop.Notice{result.Notice}
	}
	return interceptResult, nil
}
