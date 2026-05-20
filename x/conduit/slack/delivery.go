package slack

import (
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"github.com/slack-go/slack"
)

// deliverTurnComplete extracts assistant text artifacts from a turn_complete
// event and posts them back to the originating Slack channel or DM.
func (c *SlackConduit) deliverTurnComplete(event loop.TurnCompleteEvent, channelID string, threadTS string, client slackPoster) error {
	if event.Turn.Role != state.RoleAssistant {
		return nil
	}

	var content strings.Builder
	for _, art := range event.Turn.Artifacts {
		if text, ok := art.(artifact.Text); ok {
			if content.Len() > 0 {
				content.WriteString("\n")
			}
			content.WriteString(text.Content)
		}
	}

	text := content.String()
	if text == "" {
		return nil
	}

	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if threadTS != "" && !isDM(channelID) {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	_, _, err := client.PostMessage(channelID, opts...)
	if err != nil {
		return fmt.Errorf("post message: %w", err)
	}
	return nil
}
