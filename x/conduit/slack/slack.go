// Package slack implements an ore I/O conduit for Slack via Socket Mode.
//
// It maps Slack conversation threads and DMs to persistent ore Thread sessions
// via Thread.Metadata, handles inbound message events with echo suppression,
// and delivers assistant text responses back into the originating Slack thread.
package slack

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/conduit"
)

type transportMode int

const (
	modeSocket transportMode = iota
	modeEventsAPI
)

// SlackConduit is a Slack Socket Mode ore I/O conduit.
type SlackConduit struct {
	mgr      *session.Manager
	botToken string
	appToken string
	mode     transportMode
}

// Option configures a SlackConduit.
type Option func(*SlackConduit)

// WithEventsAPI switches the conduit to HTTP Events API mode.
// This is a stub for future implementation; the zero-option default is Socket Mode.
func WithEventsAPI() Option {
	return func(c *SlackConduit) {
		c.mode = modeEventsAPI
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
// The returned value must be started with Start(ctx) to begin the Socket Mode connection.
func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error) {
	if mgr == nil {
		return nil, fmt.Errorf("session manager is required")
	}
	c := &SlackConduit{
		mgr:  mgr,
		mode: modeSocket,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Start initializes the Slack connection, subscribes to output events,
// and blocks until ctx is cancelled or a fatal error occurs.
func (c *SlackConduit) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
