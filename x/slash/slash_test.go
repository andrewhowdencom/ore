package slash

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_BindAndMatch(t *testing.T) {
	r := NewRegistry()
	called := false
	r.Bind("new", "Create a new session", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		called = true
		assert.Equal(t, "new", cmd.Name)
		assert.Empty(t, cmd.Input)
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "/new"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	assert.True(t, called, "expected handler to be called")
}

func TestRegistry_UnknownCommand(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		t.Fatal("handler should not be called for unknown command")
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "/other"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed for unknown command")
	require.Len(t, result.Feedback, 1)
	assert.Contains(t, result.Feedback[0].Content, "Unknown command: /other")
	assert.Contains(t, result.Feedback[0].Content, "/help")
}

func TestRegistry_HandlerError(t *testing.T) {
	r := NewRegistry()
	expectedErr := errors.New("handler error")
	r.Bind("fail", "Fail intentionally", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{}, expectedErr
	})

	event := session.UserMessageEvent{Content: "/fail"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Equal(t, event, result.Event, "error should preserve original event")
	assert.Empty(t, result.Feedback)
}

func TestRegistry_RawInputParsing(t *testing.T) {
	r := NewRegistry()
	var capturedCmd Command
	r.Bind("include", "Include a file", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		capturedCmd = cmd
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "/include /path/with spaces"}
	result, err := r.Intercept(context.Background(), event, nil)

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
	r.Bind("status", "Show status", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{
			Feedback: artifact.Text{Content: "System status: OK"},
		}, nil
	})

	event := session.UserMessageEvent{Content: "/status"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	require.Len(t, result.Feedback, 1)
	assert.Equal(t, "System status: OK", result.Feedback[0].Content)
}

func TestRegistry_FeedbackWithReplace(t *testing.T) {
	r := NewRegistry()
	r.Bind("switch", "Switch session", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{
			Replace:  session.SessionSwitchEvent{SessionID: "new-session-123", Ctx: context.Background()},
			Feedback: artifact.Text{Content: "Switched to session new-session-123"},
		}, nil
	})

	event := session.UserMessageEvent{Content: "/switch"}
	result, err := r.Intercept(context.Background(), event, nil)

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
	r.Bind("new", "Create a new session", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		t.Fatal("handler should not be called for non-UserMessageEvent")
		return Result{}, nil
	})

	event := session.InterruptEvent{Ctx: context.Background()}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Equal(t, event, result.Event, "expected event to pass through")
	assert.Empty(t, result.Feedback)
}

func TestRegistry_NoSlashPrefix(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		t.Fatal("handler should not be called for text without slash prefix")
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "new"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Equal(t, event, result.Event, "expected event to pass through")
	assert.Empty(t, result.Feedback)
}

func TestRegistry_EmptyContent(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		t.Fatal("handler should not be called for empty content")
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: ""}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Equal(t, event, result.Event, "expected event to pass through")
	assert.Empty(t, result.Feedback)
}

func TestRegistry_HandlerReturnsEvent(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{
			Replace: session.SessionSwitchEvent{
				SessionID: "new-session-123",
				Ctx:       context.Background(),
			},
		}, nil
	})

	event := session.UserMessageEvent{Content: "/new"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	require.NotNil(t, result.Event, "expected event to be replaced, not consumed")
	assert.Empty(t, result.Feedback)

	switchEvent, ok := result.Event.(session.SessionSwitchEvent)
	require.True(t, ok, "expected SessionSwitchEvent")
	assert.Equal(t, "new-session-123", switchEvent.SessionID)
}

func TestRegistry_Help(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", "Create a new session", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{}, nil
	})
	r.Bind("compact", "Compact conversation history", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "/help"}
	result, err := r.Intercept(context.Background(), event, nil)

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
	result, err := r.Intercept(context.Background(), event, nil)

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

func TestRegistry_PostSlashWhitespace(t *testing.T) {
	r := NewRegistry()
	var capturedCmd Command
	r.Bind("help", "Show help", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		capturedCmd = cmd
		return Result{}, nil
	})

	// Multiple spaces after the slash — command should be parsed correctly.
	event := session.UserMessageEvent{Content: "/   help"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	assert.Equal(t, "help", capturedCmd.Name)
	assert.Empty(t, capturedCmd.Input)
}

func TestRegistry_PostSlashWhitespace_WithInput(t *testing.T) {
	r := NewRegistry()
	var capturedCmd Command
	r.Bind("include", "Include a file", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		capturedCmd = cmd
		return Result{}, nil
	})

	// Multiple spaces after slash and between command and input.
	event := session.UserMessageEvent{Content: "/   include   /path/with spaces"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	assert.Equal(t, "include", capturedCmd.Name)
	assert.Equal(t, "  /path/with spaces", capturedCmd.Input)
}

func TestRegistry_DuplicateBind_Overwrites(t *testing.T) {
	r := NewRegistry()
	firstCalled := false
	r.Bind("test", "First test", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		firstCalled = true
		return Result{}, nil
	})
	r.Bind("test", "Second test", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{Feedback: artifact.Text{Content: "second handler"}}, nil
	})

	event := session.UserMessageEvent{Content: "/test"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.False(t, firstCalled, "first handler should not be called")
	require.Len(t, result.Feedback, 1)
	assert.Equal(t, "second handler", result.Feedback[0].Content)
}

func TestRegistry_DuplicateBind_UpdatesDescription(t *testing.T) {
	r := NewRegistry()
	r.Bind("cmd", "Original cmd", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{}, nil
	})
	r.Bind("cmd", "Updated cmd", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{}, nil
	})

	// Verify /help shows the updated description.
	event := session.UserMessageEvent{Content: "/help"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	require.Len(t, result.Feedback, 1)
	content := result.Feedback[0].Content
	assert.Contains(t, content, "/cmd")
	assert.Contains(t, content, "Updated cmd")
	assert.NotContains(t, content, "Original cmd")
}

func TestRegistry_MixedCase(t *testing.T) {
	r := NewRegistry()
	r.Bind("help", "Show help", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "/HeLp"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event)
	require.Len(t, result.Feedback, 1)
	assert.Contains(t, result.Feedback[0].Content, "Unknown command: /HeLp")
}

func TestRegistry_EmptyFeedback(t *testing.T) {
	r := NewRegistry()
	r.Bind("silent", "Silent command", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{Feedback: artifact.Text{Content: ""}}, nil
	})

	event := session.UserMessageEvent{Content: "/silent"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	assert.Empty(t, result.Feedback, "empty feedback should not be emitted")
}

func TestRegistry_FieldsWhitespace(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, Fields("  a   b  c  "), "leading/trailing whitespace should be trimmed")
	assert.Equal(t, []string{}, Fields("   "), "only whitespace should produce empty slice")
	assert.Equal(t, []string{"a"}, Fields("a"), "no whitespace should return single element")
	assert.Equal(t, []string{"a", "b"}, Fields("a\tb\n"), "tabs and newlines should split")
}

func TestRegistry_Isolation(t *testing.T) {
	r1 := NewRegistry()
	r1.Bind("foo", "Foo command", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{}, nil
	})

	// r2 is a fresh registry and should not have the "foo" command.
	r2 := NewRegistry()

	event := session.UserMessageEvent{Content: "/foo"}
	result, err := r2.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event)
	require.Len(t, result.Feedback, 1)
	assert.Contains(t, result.Feedback[0].Content, "Unknown command: /foo")
}

func TestRegistry_LeadingWhitespace(t *testing.T) {
	r := NewRegistry()
	var capturedCmd Command
	r.Bind("help", "Show help", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		capturedCmd = cmd
		return Result{}, nil
	})

	event := session.UserMessageEvent{Content: "   /help"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	assert.Equal(t, "help", capturedCmd.Name)
	assert.Empty(t, capturedCmd.Input)
}

func TestRegistry_MultipleFeedback(t *testing.T) {
	r := NewRegistry()
	r.Bind("multi", "Multiple feedback", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{
			Feedback: artifact.Text{Content: "first"},
		}, nil
	})

	// Note: the Handler type only has a single Feedback field, not a slice.
	// The session.Interceptor interface uses a slice, so the registry returns
	// multiple feedback items by having a single handler return one item.
	// This test verifies that the single feedback item is correctly passed through.
	event := session.UserMessageEvent{Content: "/multi"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed")
	require.Len(t, result.Feedback, 1)
	assert.Equal(t, "first", result.Feedback[0].Content)
}

func TestRegistry_CaseSensitive(t *testing.T) {
	r := NewRegistry()
	r.Bind("help", "Show help", func(ctx context.Context, emitter loop.Emitter, cmd Command) (Result, error) {
		return Result{}, nil
	})

	// Uppercase HELP should be treated as unknown command.
	event := session.UserMessageEvent{Content: "/HELP"}
	result, err := r.Intercept(context.Background(), event, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Event, "expected event to be consumed for unknown command")
	require.Len(t, result.Feedback, 1)
	assert.Contains(t, result.Feedback[0].Content, "Unknown command: /HELP")
}
