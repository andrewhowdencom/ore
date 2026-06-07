package slash

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_BindAndMatch(t *testing.T) {
	r := NewRegistry()
	called := false
	r.Bind("new", "Create a new session", func(ctx context.Context, cmd Command) (Result, error) {
		called = true
		assert.Equal(t, "new", cmd.Name)
		assert.Empty(t, cmd.Input)
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "/new"}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	assert.True(t, called, "expected handler to be called")
}

func TestRegistry_UnknownCommand(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, cmd Command) (Result, error) {
		t.Fatal("handler should not be called for unknown command")
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "/other"}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed for unknown command")
	require.Len(t, result.Feedback, 1)
	assert.Contains(t, result.Feedback[0].Content, "Unknown command: /other")
	assert.Contains(t, result.Feedback[0].Content, "/help")
}

func TestRegistry_HandlerError(t *testing.T) {
	r := NewRegistry()
	expectedErr := errors.New("handler error")
	r.Bind("fail", "Fail intentionally", func(ctx context.Context, cmd Command) (Result, error) {
		return Result{}, expectedErr
	})

	event := session.UserMessageEvent{Content: "/fail"}
	result, err := r.Intercept(context.Background(), event)

	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Equal(t, event, result.Event, "error should preserve original event")
	assert.Empty(t, result.Feedback)
}

func TestRegistry_RawInputParsing(t *testing.T) {
	r := NewRegistry()
	var capturedCmd Command
	r.Bind("include", "Include a file", func(ctx context.Context, cmd Command) (Result, error) {
		capturedCmd = cmd
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "/include /path/with spaces"}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	assert.Equal(t, "include", capturedCmd.Name)
	assert.Equal(t, "/path/with spaces", capturedCmd.Input)
}

func TestRegistry_Fields(t *testing.T) {
	fields := Fields("/path/with spaces  and\ttabs")
	assert.Equal(t, []string{"/path/with", "spaces", "and", "tabs"}, fields)
}

func TestRegistry_Feedback(t *testing.T) {
	r := NewRegistry()
	r.Bind("status", "Show status", func(ctx context.Context, cmd Command) (Result, error) {
		return Result{
			Feedback: artifact.Text{Content: "System status: OK"},
		}, nil
	})

	event := session.UserMessageEvent{Content: "/status"}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	require.Len(t, result.Feedback, 1)
	assert.Equal(t, "System status: OK", result.Feedback[0].Content)
}

func TestRegistry_FeedbackWithReplace(t *testing.T) {
	r := NewRegistry()
	r.Bind("switch", "Switch session", func(ctx context.Context, cmd Command) (Result, error) {
		return Result{
			Replace:  session.SessionSwitchEvent{SessionID: "new-session-123", Ctx: context.Background()},
			Feedback: artifact.Text{Content: "Switched to session new-session-123"},
		}, nil
	})

	event := session.UserMessageEvent{Content: "/switch"}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	require.NotNil(t, result.Event)
	assert.Len(t, result.Feedback, 1)
	assert.Equal(t, "Switched to session new-session-123", result.Feedback[0].Content)

	switchEvent, ok := result.Event.(session.SessionSwitchEvent)
	require.True(t, ok, "expected SessionSwitchEvent")
	assert.Equal(t, "new-session-123", switchEvent.SessionID)
}

func TestRegistry_NonUserMessage(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, cmd Command) (Result, error) {
		t.Fatal("handler should not be called for non-UserMessageEvent")
		return Result{}, nil
	})

	event := session.InterruptEvent{Ctx: context.Background()}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.Equal(t, event, result.Event, "expected event to pass through")
	assert.Empty(t, result.Feedback)
}

func TestRegistry_NoSlashPrefix(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, cmd Command) (Result, error) {
		t.Fatal("handler should not be called for text without slash prefix")
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "new"}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.Equal(t, event, result.Event, "expected event to pass through")
	assert.Empty(t, result.Feedback)
}

func TestRegistry_EmptyContent(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, cmd Command) (Result, error) {
		t.Fatal("handler should not be called for empty content")
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: ""}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.Equal(t, event, result.Event, "expected event to pass through")
	assert.Empty(t, result.Feedback)
}

func TestRegistry_HandlerReturnsEvent(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, cmd Command) (Result, error) {
		return Result{
			Replace: session.SessionSwitchEvent{
				SessionID: "new-session-123",
				Ctx:       context.Background(),
			},
		}, nil
	})

	event := session.UserMessageEvent{Content: "/new"}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	require.NotNil(t, result.Event, "expected event to be replaced, not consumed")
	assert.Empty(t, result.Feedback)

	switchEvent, ok := result.Event.(session.SessionSwitchEvent)
	require.True(t, ok, "expected SessionSwitchEvent")
	assert.Equal(t, "new-session-123", switchEvent.SessionID)
}

func TestRegistry_Help(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, cmd Command) (Result, error) {
		return Result{}, nil
	})
	r.Bind("compact", "Compact conversation history", func(ctx context.Context, cmd Command) (Result, error) {
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "/help"}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	require.Len(t, result.Feedback, 1)

	content := result.Feedback[0].Content
	assert.Contains(t, content, "Available commands:")
	assert.Contains(t, content, "/new")
	assert.Contains(t, content, "Create a new session")
	assert.Contains(t, content, "/compact")
	assert.Contains(t, content, "Compact conversation history")
	assert.Contains(t, content, "/help")
	assert.Contains(t, content, "Show available slash commands")
}

func TestRegistry_HelpExcludesUnbound(t *testing.T) {
	r := NewRegistry()
	// Only /help is auto-registered; no other commands.

	event := session.UserMessageEvent{Content: "/help"}
	result, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	require.Len(t, result.Feedback, 1)

	content := result.Feedback[0].Content
	assert.Contains(t, content, "Available commands:")
	assert.Contains(t, content, "/help")
	// Should not contain any other commands.
	count := strings.Count(content, "\n")
	assert.Equal(t, 1, count, "expected 2 lines (header + /help) with 1 newline")
}

func TestRegistry_CompileTimeAssertion(t *testing.T) {
	// Verify that the registry struct implements session.Interceptor.
	var _ session.Interceptor = (*registry)(nil)
}
