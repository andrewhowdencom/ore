package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/slack-go/slack/slackevents"
	"go.opentelemetry.io/otel/trace"
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
	stream, err := c.resolveThread(slackThreadID, event.Channel)
	if err != nil {
		return err
	}

	turnCtx := ctx
	var span trace.Span
	if c.tracer != nil {
		turnCtx, span = c.tracer.Start(turnCtx, "slack.turn", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
	}

	userEvent := session.UserMessageEvent{
		Content: event.Text,
		Ctx:     loop.WithProvenance(turnCtx, "slack"),
	}

	if err := stream.Submit(userEvent); err != nil {
		return fmt.Errorf("submit message: %w", err)
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
