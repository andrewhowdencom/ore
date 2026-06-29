package slack

import (
	"context"
	"fmt"
	"github.com/andrewhowdencom/ore/models"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a provider.Provider implementation for testing.
type mockProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (m *mockProvider) Invoke(ctx context.Context, s ledger.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	for _, art := range m.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

// simpleProcessor runs a single Step.Turn with the mock provider.
func simpleProcessor() junk.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st ledger.State, prov provider.Provider, _ models.Spec) (ledger.State, error) {
		spec := models.Spec{Name: "test-model"}
		return step.Turn(ctx, st, spec, prov)
	}
}

func TestNew_NilManager(t *testing.T) {
	_, err := New(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session manager is required")
}

func TestNew_ValidManager(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestDescriptor(t *testing.T) {
	assert.NotNil(t, Descriptor)
	assert.Equal(t, "Slack", Descriptor.Name)
	assert.Equal(t, "Slack Socket Mode conduit", Descriptor.Description)
	assert.Equal(t, []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapRenderTurn,
		conduit.CapAcceptText,
	}, Descriptor.Capabilities)
}

func TestWithEventsAPI(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr, WithEventsAPI())
	require.NoError(t, err)
	require.NotNil(t, c)
}

// mockSlackClient is a test double for slackClient.
type mockSlackClient struct {
	botUserID string
	authErr   error
}

func (m *mockSlackClient) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	return "", "", nil
}

func (m *mockSlackClient) AuthTest() (*slack.AuthTestResponse, error) {
	if m.authErr != nil {
		return nil, m.authErr
	}
	return &slack.AuthTestResponse{UserID: m.botUserID}, nil
}

// fakeSocketModeClient is a test double for socketModeClient.
type fakeSocketModeClient struct {
	events   chan socketmode.Event
	runDone  chan struct{}
	stopOnce sync.Once
}

func (f *fakeSocketModeClient) Run() error {
	<-f.runDone
	return nil
}

func (f *fakeSocketModeClient) Events() <-chan socketmode.Event {
	return f.events
}

func (f *fakeSocketModeClient) Ack(req socketmode.Request, payload ...interface{}) error {
	return nil
}

func (f *fakeSocketModeClient) stop() {
	f.stopOnce.Do(func() {
		close(f.runDone)
		close(f.events)
	})
}

func TestStart_MissingTokens(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SLACK_BOT_TOKEN and SLACK_APP_TOKEN are required")
}

func TestStart_EventsAPIStub(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	c, err := New(mgr, WithEventsAPI(), WithBotToken("test"), WithAppToken("test"))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Events API mode is not yet implemented")
}

func TestStart_BlocksUntilContextCancelled(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	fakeSMC := &fakeSocketModeClient{
		events:  make(chan socketmode.Event),
		runDone: make(chan struct{}),
	}
	t.Cleanup(fakeSMC.stop)

	mockSlack := &mockSlackClient{botUserID: "B123"}

	c, err := New(mgr,
		WithBotToken("test"),
		WithAppToken("test"),
		WithSlackClient(mockSlack),
		WithSocketModeClient(fakeSMC),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.Start(ctx)
	require.NoError(t, err)
}

func TestStart_AuthTestFailure(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	fakeSMC := &fakeSocketModeClient{
		events:  make(chan socketmode.Event),
		runDone: make(chan struct{}),
	}
	t.Cleanup(fakeSMC.stop)

	mockSlack := &mockSlackClient{authErr: fmt.Errorf("invalid_auth")}

	c, err := New(mgr,
		WithBotToken("test"),
		WithAppToken("test"),
		WithSlackClient(mockSlack),
		WithSocketModeClient(fakeSMC),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = c.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slack auth test")
}
