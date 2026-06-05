package slash

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_BindAndMatch(t *testing.T) {
	r := NewRegistry()
	called := false
	r.Bind("new", func(ctx context.Context, args []string) (session.Event, error) {
		called = true
		return nil, nil
	})

	event := session.UserMessageEvent{Content: "/new"}
	_, consumed, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.True(t, consumed, "expected event to be consumed")
	assert.True(t, called, "expected handler to be called")
}

func TestRegistry_NoMatch(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", func(ctx context.Context, args []string) (session.Event, error) {
		t.Fatal("handler should not be called for non-matching command")
		return nil, nil
	})

	event := session.UserMessageEvent{Content: "/other"}
	newEvent, consumed, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.False(t, consumed, "expected event to pass through")
	assert.Equal(t, event, newEvent)
}

func TestRegistry_HandlerError(t *testing.T) {
	r := NewRegistry()
	expectedErr := errors.New("handler error")
	r.Bind("fail", func(ctx context.Context, args []string) (session.Event, error) {
		return nil, expectedErr
	})

	event := session.UserMessageEvent{Content: "/fail"}
	_, consumed, err := r.Intercept(context.Background(), event)

	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.False(t, consumed, "error should not consume event")
}

func TestRegistry_ArgsParsing(t *testing.T) {
	r := NewRegistry()
	var capturedArgs []string
	r.Bind("new", func(ctx context.Context, args []string) (session.Event, error) {
		capturedArgs = args
		return nil, nil
	})

	event := session.UserMessageEvent{Content: "/new arg1 arg2"}
	_, consumed, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.True(t, consumed)
	assert.Equal(t, []string{"arg1", "arg2"}, capturedArgs)
}

func TestRegistry_NonUserMessage(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", func(ctx context.Context, args []string) (session.Event, error) {
		t.Fatal("handler should not be called for non-UserMessageEvent")
		return nil, nil
	})

	event := session.InterruptEvent{Ctx: context.Background()}
	newEvent, consumed, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.False(t, consumed, "expected event to pass through")
	assert.Equal(t, event, newEvent)
}

func TestRegistry_NoSlashPrefix(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", func(ctx context.Context, args []string) (session.Event, error) {
		t.Fatal("handler should not be called for text without slash prefix")
		return nil, nil
	})

	event := session.UserMessageEvent{Content: "new"}
	newEvent, consumed, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.False(t, consumed, "expected event to pass through")
	assert.Equal(t, event, newEvent)
}

func TestRegistry_EmptyContent(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", func(ctx context.Context, args []string) (session.Event, error) {
		t.Fatal("handler should not be called for empty content")
		return nil, nil
	})

	event := session.UserMessageEvent{Content: ""}
	newEvent, consumed, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.False(t, consumed, "expected event to pass through")
	assert.Equal(t, event, newEvent)
}

func TestRegistry_HandlerReturnsEvent(t *testing.T) {
	r := NewRegistry()
	r.Bind("new", func(ctx context.Context, args []string) (session.Event, error) {
		return session.SessionSwitchEvent{
			SessionID: "new-session-123",
			Ctx:       context.Background(),
		}, nil
	})

	event := session.UserMessageEvent{Content: "/new"}
	newEvent, consumed, err := r.Intercept(context.Background(), event)

	require.NoError(t, err)
	assert.False(t, consumed, "expected event to be replaced, not consumed")

	switchEvent, ok := newEvent.(session.SessionSwitchEvent)
	require.True(t, ok, "expected SessionSwitchEvent")
	assert.Equal(t, "new-session-123", switchEvent.SessionID)
}

func TestRegistry_CompileTimeAssertion(t *testing.T) {
	// Verify that the registry struct implements session.Interceptor.
	var _ session.Interceptor = (*registry)(nil)
}
