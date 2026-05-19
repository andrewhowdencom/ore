package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/slack-go/slack/slackevents"
)

// handleMessageEvent processes a Slack message event: it filters, resolves
// the corresponding ore thread, and submits a UserMessageEvent to the stream.
func (c *SlackConduit) handleMessageEvent(ctx context.Context, event *slackevents.MessageEvent, botUserID string) error {
	// Echo suppression: skip the bot's own messages.
	if event.User == botUserID {
		return nil
	}

	// Addressing filter: DMs are always addressed; channels require @mention.
	if !isAddressedToBot(event, botUserID) {
		return nil
	}

	slackThreadID := slackThreadIDFromEvent(event)
	stream, _, err := c.resolveThread(slackThreadID, event.Channel)
	if err != nil {
		return err
	}

	userEvent := session.UserMessageEvent{
		Content: event.Text,
		Ctx:     loop.EventContext{Provenance: "slack"},
	}

	if err := stream.Process(ctx, userEvent); err != nil {
		if errors.Is(err, session.ErrSessionBusy) {
			slog.Error("session busy", "thread", slackThreadID, "err", err)
			return nil
		}
		return fmt.Errorf("process message: %w", err)
	}

	return nil
}

// isAddressedToBot returns true if the message is directed at the bot.
// DMs are implicitly addressed; channel messages must contain a bot mention.
func isAddressedToBot(event *slackevents.MessageEvent, botUserID string) bool {
	if isDM(event.Channel) {
		return true
	}
	mention := fmt.Sprintf("<@%s>", botUserID)
	return strings.Contains(event.Text, mention)
}
