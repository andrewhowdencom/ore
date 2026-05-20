package slack

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSlackClient creates a slack.Client that sends requests to an
// httptest server and returns the client and a pointer to capture the
// last received form values.
func newTestSlackClient(t *testing.T) (*slack.Client, *url.Values, *httptest.Server) {
	var receivedForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var parseErr error
		receivedForm, parseErr = url.ParseQuery(string(body))
		if parseErr != nil {
			t.Fatalf("parse query: %v", parseErr)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	t.Cleanup(server.Close)

	client := slack.New("test-token", slack.OptionAPIURL(server.URL+"/"))
	return client, &receivedForm, server
}

func TestDeliverTurnComplete_TextArtifact(t *testing.T) {
	client, receivedForm, _ := newTestSlackClient(t)

	event := loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "Hello world"},
			},
		},
	}

	c := &SlackConduit{}
	err := c.deliverTurnComplete(event, "C123", "1234567890.123456", client)
	require.NoError(t, err)

	assert.Equal(t, "Hello world", (*receivedForm).Get("text"))
	assert.Equal(t, "1234567890.123456", (*receivedForm).Get("thread_ts"))
}

func TestDeliverTurnComplete_DM(t *testing.T) {
	client, receivedForm, _ := newTestSlackClient(t)

	event := loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "DM reply"},
			},
		},
	}

	c := &SlackConduit{}
	err := c.deliverTurnComplete(event, "D123", "D123", client)
	require.NoError(t, err)

	assert.Equal(t, "DM reply", (*receivedForm).Get("text"))
	assert.Equal(t, "", (*receivedForm).Get("thread_ts"), "DMs should not set thread_ts")
}

func TestDeliverTurnComplete_NoTextArtifacts(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	client := slack.New("test-token", slack.OptionAPIURL(server.URL+"/"))

	event := loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Image{URL: "http://example.com/image.png"},
			},
		},
	}

	c := &SlackConduit{}
	err := c.deliverTurnComplete(event, "C123", "1234567890.123456", client)
	require.NoError(t, err)
	assert.False(t, called, "no HTTP request should be made when there are no text artifacts")
}

func TestDeliverTurnComplete_NonAssistantRole(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	client := slack.New("test-token", slack.OptionAPIURL(server.URL+"/"))

	event := loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "User message"},
			},
		},
	}

	c := &SlackConduit{}
	err := c.deliverTurnComplete(event, "C123", "1234567890.123456", client)
	require.NoError(t, err)
	assert.False(t, called, "no HTTP request should be made for non-assistant turns")
}

func TestDeliverTurnComplete_MultipleTextArtifacts(t *testing.T) {
	client, receivedForm, _ := newTestSlackClient(t)

	event := loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "First paragraph"},
				artifact.Text{Content: "Second paragraph"},
			},
		},
	}

	c := &SlackConduit{}
	err := c.deliverTurnComplete(event, "C123", "1234567890.123456", client)
	require.NoError(t, err)

	assert.Equal(t, "First paragraph\nSecond paragraph", (*receivedForm).Get("text"))
}

func TestDeliverTurnComplete_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok": false, "error": "internal_error"}`))
	}))
	defer server.Close()

	client := slack.New("test-token", slack.OptionAPIURL(server.URL+"/"))

	event := loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "Hello"},
			},
		},
	}

	c := &SlackConduit{}
	err := c.deliverTurnComplete(event, "C123", "1234567890.123456", client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "post message")
}
