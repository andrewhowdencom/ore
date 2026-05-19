package slack

import "github.com/slack-go/slack"

// slackPoster is the subset of slack.Client needed to post messages.
type slackPoster interface {
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
}

// slackAuthTester is the subset of slack.Client needed to test authentication.
type slackAuthTester interface {
	AuthTest() (*slack.AuthTestResponse, error)
}
