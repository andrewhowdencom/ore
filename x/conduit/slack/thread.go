package slack

import (
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/slack-go/slack/slackevents"
)

// resolveThread looks up or creates an ore Thread mapped to the given
// Slack thread identifier, then returns the active session Stream and
// the underlying Thread.
func (c *SlackConduit) resolveThread(slackThreadID string) (*session.Stream, *thread.Thread, error) {
	store := c.mgr.Store()

	// Try to resume an existing thread by slack_thread_id metadata.
	if thr, ok := store.GetBy("slack_thread_id", slackThreadID); ok {
		stream, err := c.mgr.Attach(thr.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("attach to thread %q: %w", thr.ID, err)
		}
		return stream, thr, nil
	}

	// No existing thread — create a new one.
	stream, err := c.mgr.Create()
	if err != nil {
		return nil, nil, fmt.Errorf("create thread: %w", err)
	}

	thr, ok := store.Get(stream.ID())
	if !ok {
		return nil, nil, fmt.Errorf("created thread %q not found in store", stream.ID())
	}

	thr.SetMetadata("slack_thread_id", slackThreadID)
	if err := store.Save(thr); err != nil {
		return nil, nil, fmt.Errorf("save thread metadata: %w", err)
	}

	return stream, thr, nil
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
