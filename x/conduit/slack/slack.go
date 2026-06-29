// Package slack implements an ore I/O conduit for Slack via Socket Mode.
//
// It maps Slack conversation threads and DMs to persistent ore Thread sessions
// via Thread.Metadata, handles inbound message events with echo suppression,
// and delivers assistant text responses back into the originating Slack thread.
package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"go.opentelemetry.io/otel/trace"
)

type transportMode int

const (
	modeSocket transportMode = iota
	modeEventsAPI
)

// socketModeClient abstracts socketmode.Client for testability.
type socketModeClient interface {
	Run() error
	Events() <-chan socketmode.Event
	Ack(req socketmode.Request, payload ...interface{}) error
}

// socketModeClientAdapter adapts *socketmode.Client to socketModeClient.
type socketModeClientAdapter struct {
	client *socketmode.Client
}

func (a *socketModeClientAdapter) Run() error {
	return a.client.Run()
}

func (a *socketModeClientAdapter) Events() <-chan socketmode.Event {
	return a.client.Events
}

func (a *socketModeClientAdapter) Ack(req socketmode.Request, payload ...interface{}) error {
	return a.client.Ack(req, payload...)
}

// SlackConduit is a Slack Socket Mode ore I/O conduit.
type SlackConduit struct {
	mgr              *junk.Manager
	botToken         string
	appToken         string
	mode             transportMode
	client           slackClient
	socketModeClient socketModeClient
	activeStreams    map[string]*junk.Stream
	streamsMu        sync.Mutex
	tracer           trace.Tracer
}

// Option configures a SlackConduit.
type Option func(*SlackConduit)

// WithBotToken sets the Slack bot token (xoxb-...).
func WithBotToken(token string) Option {
	return func(c *SlackConduit) {
		c.botToken = token
	}
}

// WithAppToken sets the Slack app-level token (xapp-...).
func WithAppToken(token string) Option {
	return func(c *SlackConduit) {
		c.appToken = token
	}
}

// WithEventsAPI switches the conduit to HTTP Events API mode.
// This is a stub for future implementation; the zero-option default is Socket Mode.
func WithEventsAPI() Option {
	return func(c *SlackConduit) {
		c.mode = modeEventsAPI
	}
}

// WithTracer configures an OpenTelemetry tracer for the Slack conduit.
func WithTracer(tracer trace.Tracer) Option {
	return func(c *SlackConduit) {
		c.tracer = tracer
	}
}

// WithSlackClient injects a Slack client for testing.
func WithSlackClient(client slackClient) Option {
	return func(c *SlackConduit) {
		c.client = client
	}
}

// WithSocketModeClient injects a Socket Mode client for testing.
func WithSocketModeClient(client socketModeClient) Option {
	return func(c *SlackConduit) {
		c.socketModeClient = client
	}
}

// Descriptor enumerates the capabilities of the Slack conduit.
var Descriptor = conduit.Descriptor{
	Name:        "Slack",
	Description: "Slack Socket Mode conduit",
	Capabilities: []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapRenderTurn,
		conduit.CapAcceptText,
	},
}

// New creates a new Slack conduit that implements conduit.Conduit.
func New(mgr *junk.Manager, opts ...Option) (conduit.Conduit, error) {
	if mgr == nil {
		return nil, fmt.Errorf("session manager is required")
	}
	c := &SlackConduit{
		mgr:           mgr,
		mode:          modeSocket,
		activeStreams: make(map[string]*junk.Stream),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Start initializes the Slack Socket Mode connection, subscribes to output
// events, and blocks until ctx is cancelled or a fatal error occurs.
func (c *SlackConduit) Start(ctx context.Context) error {
	botToken := c.botToken
	if botToken == "" {
		botToken = os.Getenv("SLACK_BOT_TOKEN")
	}
	appToken := c.appToken
	if appToken == "" {
		appToken = os.Getenv("SLACK_APP_TOKEN")
	}
	if botToken == "" || appToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN and SLACK_APP_TOKEN are required")
	}

	if c.mode == modeEventsAPI {
		return fmt.Errorf("Events API mode is not yet implemented")
	}

	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))

	var slackAPI slackClient
	if c.client != nil {
		slackAPI = c.client
	} else {
		slackAPI = api
	}

	authResp, err := slackAPI.AuthTest()
	if err != nil {
		return fmt.Errorf("slack auth test: %w", err)
	}
	botUserID := authResp.UserID

	// Register sink for turn_complete events from Slack-originated threads.
	sink := func(streamID string, event loop.OutputEvent) {
		tc, ok := event.(loop.TurnCompleteEvent)
		if !ok || tc.Turn.Role != ledger.RoleAssistant {
			return
		}
		p, _ := loop.ProvenanceFrom(tc.Ctx)
		if p != "slack" {
			return
		}
		stream, err := c.mgr.Get(streamID)
		if err != nil {
			return
		}
		channelID, _ := stream.GetMetadata("slack_channel_id")
		threadTS, _ := stream.GetMetadata("slack_thread_id")
		if channelID == "" {
			return
		}
		if err := c.deliverTurnComplete(tc, channelID, threadTS, slackAPI); err != nil {
			slog.Error("deliver turn complete", "err", err, "stream", streamID)
		}
	}
	unregister := c.mgr.RegisterSink([]string{"turn_complete"}, sink)
	defer unregister()

	// Start Socket Mode.
	var smc socketModeClient
	if c.socketModeClient != nil {
		smc = c.socketModeClient
	} else {
		smc = &socketModeClientAdapter{client: socketmode.New(api)}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- smc.Run()
	}()

	// Process incoming events.
	go func() {
		for evt := range smc.Events() {
			if evt.Request != nil {
				_ = smc.Ack(*evt.Request)
			}
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				ev, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				switch e := ev.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					if err := c.handleMessageEvent(ctx, e, botUserID); err != nil {
						slog.Error("handle message", "err", err)
					}
				}
			}
		}
	}()

	// Block until ctx is cancelled or Socket Mode fails.
	select {
	case <-ctx.Done():
		c.streamsMu.Lock()
		for _, stream := range c.activeStreams {
			_ = stream.Close()
		}
		c.streamsMu.Unlock()
		return nil
	case err := <-errCh:
		return fmt.Errorf("socket mode: %w", err)
	}
}
