package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct{}

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	return nil
}

func testManager(t *testing.T) *session.Manager {
	t.Helper()
	return session.NewManager(
		thread.NewMemoryStore(),
		&mockProvider{},
		func() *loop.Step { return loop.New() },
		func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
			return step.Submit(ctx, st, state.RoleAssistant, artifact.Text{Content: "Test reply"})
		},
	)
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		mgr     *session.Manager
		wantErr bool
	}{
		{"nil manager", nil, true},
		{"valid manager", testManager(t), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.mgr)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, c)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, c)
			}
		})
	}
}

func TestStart_MissingToken(t *testing.T) {
	mgr := testManager(t)
	c, err := New(mgr)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bot token is required")
}

func TestStart_InvalidToken(t *testing.T) {
	token := "invalid-token"
	prefix := "/bot" + token + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prefix+"getMe" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 401, "description": "Unauthorized"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	mgr := testManager(t)
	c, err := New(mgr, WithBotToken(token), withBaseURL(srv.URL))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate bot token")
}

func TestStart_GracefulShutdown(t *testing.T) {
	token := "test-token"
	prefix := "/bot" + token + "/"
	getMeCalled := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prefix + "getMe":
			select {
			case getMeCalled <- struct{}{}:
			default:
			}
			_ = json.NewEncoder(w).Encode(getMeResp{OK: true, Result: user{ID: 42, IsBot: true}})
		case prefix + "getUpdates":
			time.Sleep(50 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(getUpdatesResp{OK: true})
		}
	}))
	defer srv.Close()

	mgr := testManager(t)
	c, err := New(mgr, WithBotToken(token), withBaseURL(srv.URL), WithGetUpdatesTimeout(1))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- c.Start(ctx)
	}()

	select {
	case <-getMeCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("getMe was not called")
	}

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-startErr:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancellation")
	}
}

func TestMessageProcessing(t *testing.T) {
	token := "test-token"
	prefix := "/bot" + token + "/"
	var updatesGiven int32
	sendMsgCh := make(chan sendMessageReq, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prefix + "getMe":
			_ = json.NewEncoder(w).Encode(getMeResp{OK: true, Result: user{ID: 42, IsBot: true}})
		case prefix + "getUpdates":
			if atomic.AddInt32(&updatesGiven, 1) == 1 {
				_ = json.NewEncoder(w).Encode(getUpdatesResp{
					OK: true,
					Result: []update{
						{
							UpdateID: 1,
							Message: &message{
								MessageID: 1,
								From:      &user{ID: 123, IsBot: false},
								Chat:      &chat{ID: 456},
								Text:      "Hello bot",
							},
						},
					},
				})
				return
			}
			time.Sleep(50 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(getUpdatesResp{OK: true})
		case prefix + "sendMessage":
			var req sendMessageReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			select {
			case sendMsgCh <- req:
			default:
			}
			_ = json.NewEncoder(w).Encode(sendMessageResp{OK: true})
		}
	}))
	defer srv.Close()

	mgr := testManager(t)
	c, err := New(mgr, WithBotToken(token), withBaseURL(srv.URL), WithGetUpdatesTimeout(1))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- c.Start(ctx)
	}()

	select {
	case req := <-sendMsgCh:
		assert.Equal(t, int64(456), req.ChatID)
		assert.Equal(t, "Test reply", req.Text)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for sendMessage")
	}

	cancel()

	select {
	case err := <-startErr:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancellation")
	}
}

func TestBotMessageSkipped(t *testing.T) {
	token := "test-token"
	prefix := "/bot" + token + "/"
	var updatesGiven int32
	sendMsgCh := make(chan sendMessageReq, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prefix + "getMe":
			_ = json.NewEncoder(w).Encode(getMeResp{OK: true, Result: user{ID: 42, IsBot: true}})
		case prefix + "getUpdates":
			if atomic.AddInt32(&updatesGiven, 1) == 1 {
				_ = json.NewEncoder(w).Encode(getUpdatesResp{
					OK: true,
					Result: []update{
						{
							UpdateID: 1,
							Message: &message{
								MessageID: 1,
								From:      &user{ID: 42, IsBot: true}, // bot's own message
								Chat:      &chat{ID: 456},
								Text:      "Bot says hello",
							},
						},
					},
				})
				return
			}
			time.Sleep(50 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(getUpdatesResp{OK: true})
		case prefix + "sendMessage":
			var req sendMessageReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			select {
			case sendMsgCh <- req:
			default:
			}
			_ = json.NewEncoder(w).Encode(sendMessageResp{OK: true})
		}
	}))
	defer srv.Close()

	mgr := testManager(t)
	c, err := New(mgr, WithBotToken(token), withBaseURL(srv.URL), WithGetUpdatesTimeout(1))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- c.Start(ctx)
	}()

	// Wait for the bot message to be processed (or skipped).
	time.Sleep(300 * time.Millisecond)

	// Verify Start is still running (did not return early with an error).
	select {
	case err := <-startErr:
		t.Fatalf("Start returned unexpectedly: %v", err)
	default:
	}

	// Verify no sendMessage was called.
	select {
	case <-sendMsgCh:
		t.Fatal("sendMessage should not be called for bot's own message")
	default:
	}

	cancel()

	select {
	case err := <-startErr:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancellation")
	}
}

func TestProvenanceFiltering(t *testing.T) {
	token := "test-token"
	prefix := "/bot" + token + "/"
	sendMsgCh := make(chan sendMessageReq, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prefix + "getMe":
			_ = json.NewEncoder(w).Encode(getMeResp{OK: true, Result: user{ID: 42, IsBot: true}})
		case prefix + "getUpdates":
			time.Sleep(50 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(getUpdatesResp{OK: true})
		case prefix + "sendMessage":
			var req sendMessageReq
			_ = json.NewDecoder(r.Body).Decode(&req)
			select {
			case sendMsgCh <- req:
			default:
			}
			_ = json.NewEncoder(w).Encode(sendMessageResp{OK: true})
		}
	}))
	defer srv.Close()

	// Create a manager with a processor that preserves the "http" provenance
	// on the assistant turn. This simulates a multi-conduit setup where another
	// conduit's events carry their own provenance.
	preservingProcessor := func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		step.SetEventContext(loop.EventContext{Provenance: "http"})
		return step.Submit(ctx, st, state.RoleAssistant, artifact.Text{Content: "Test reply"})
	}

	mgr := session.NewManager(
		thread.NewMemoryStore(),
		&mockProvider{},
		func() *loop.Step { return loop.New() },
		preservingProcessor,
	)

	c, err := New(mgr, WithBotToken(token), withBaseURL(srv.URL), WithGetUpdatesTimeout(1))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	startErr := make(chan error, 1)
	go func() {
		startErr <- c.Start(ctx)
	}()

	// Wait for getMe to complete.
	time.Sleep(200 * time.Millisecond)

	// Create a stream and process a message with "http" provenance.
	// The preservingProcessor will set the assistant turn's provenance to "http".
	stream, err := mgr.Create()
	require.NoError(t, err)

	err = stream.Process(ctx, session.UserMessageEvent{
		Content: "Hello from HTTP",
		Ctx:     loop.EventContext{Provenance: "http"},
	})
	require.NoError(t, err)

	// Wait for processing.
	time.Sleep(200 * time.Millisecond)

	// Verify sendMessage was NOT called because provenance is "http", not "telegram".
	select {
	case <-sendMsgCh:
		t.Fatal("sendMessage should not be called for non-telegram provenance")
	default:
	}

	cancel()

	select {
	case err := <-startErr:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancellation")
	}
}
