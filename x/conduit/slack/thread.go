package slack

import (
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/session"
	"github.com/slack-go/slack/slackevents"
)

// resolveThread looks up or creates an ore Thread mapped to the given
// Slack thread identifier and channel ID, then returns the active session
// Stream.
func (c *SlackConduit) resolveThread(slackThreadID string, channelID string) (*session.Stream, error) {
	// Try to resume an existing thread by slack_thread_id metadata.
	if thr, ok := c.mgr.GetBy("slack_thread_id", slackThreadID); ok {
		stream, err := c.mgr.Attach(thr.ID)
		if err != nil {
			return nil, fmt.Errorf("attach to thread %q: %w", thr.ID, err)
		}
		c.streamsMu.Lock()
		c.activeStreams[stream.ID()] = stream
		c.streamsMu.Unlock()
		return stream, nil
	}

	// No existing thread — create a new one.
	stream, err := c.mgr.Create()
	if err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}

	stream.SetMetadata("slack_thread_id", slackThreadID)
	stream.SetMetadata("slack_channel_id", channelID)
	if err := stream.Save(); err != nil {
		return nil, fmt.Errorf("save thread metadata: %w", err)
	}

	c.streamsMu.Lock()
	c.activeStreams[stream.ID()] = stream
	c.streamsMu.Unlock()

	return stream, nil
}

// slackThreadIDFromEvent extracts the ore thread identifier from a Slack
// message event. For DMs the channel ID is used; for channel threads the
// thread_ts (or top-level ts) is used.
func slackThreadIDFromEvent(event *slackevents.MessageEvent) string {
	if isDM(event.Channel) {
		return event.Channel
	}
	if event.ThreadTimeStamp != "" {
		return event.ThreadTimeStamp
	}
	return event.TimeStamp
}

// isDM returns true if the Slack channel ID represents a direct message.
func isDM(channelID string) bool {
	return strings.HasPrefix(channelID, "D")
}
