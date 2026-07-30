package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/engine"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/session"
	xhttp "github.com/andrewhowdencom/ore/x/conduit/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationTest wires a real engine.Engine, a session.Registry,
// and a ledger.MemoryRepository to the x/conduit/http conduit.
// It is the canonical integration test for the engine + HTTP migration.
type integrationTest struct {
	mu       sync.Mutex
	registry session.Registry
	repo     ledger.Repository
	eng      *engine.Engine
}

func (i *integrationTest) CreateSession(ctx context.Context, threadID string) (*session.Session, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	var sess *session.Session
	if threadID == "" {
		sess = session.New("sess-"+time.Now().Format("20060102150405.000000000"), ledger.NewThread())
	} else {
		turns, tip, err := i.repo.HydrateThread(ctx, threadID)
		if err != nil {
			return nil, err
		}
		th := ledger.NewThread()
		for _, t := range turns {
			th.SaveTurn(t)
		}
		th.SetCurrentTip(tip)
		sess = session.New(threadID, th)
	}
	if err := i.registry.Register(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (i *integrationTest) GetSession(ctx context.Context, id string) (*session.Session, error) {
	return i.registry.Get(id)
}

func (i *integrationTest) Submit(ctx context.Context, id string, event session.Event) error {
	return i.eng.Submit(ctx, id, event)
}

func (i *integrationTest) ListThreads(ctx context.Context) ([]xhttp.ThreadSummary, error) {
	ids, err := i.repo.ListThreadIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]xhttp.ThreadSummary, 0, len(ids))
	for _, id := range ids {
		turns, _, err := i.repo.HydrateThread(ctx, id)
		if err != nil {
			return nil, err
		}
		var lastAt time.Time
		for _, t := range turns {
			if t.Timestamp.After(lastAt) {
				lastAt = t.Timestamp
			}
		}
		out = append(out, xhttp.ThreadSummary{ID: id, LastAt: lastAt})
	}
	return out, nil
}

func (i *integrationTest) DeleteSession(ctx context.Context, id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	s, err := i.registry.Remove(id)
	if err != nil {
		return err
	}
	_ = s.Close()
	return nil
}

// startTestServer wires the full stack and returns the underlying
// httptest.Server plus a context-cancel cleanup. The test must
// defer the cleanup to close the SSE handlers.
func startTestServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	registry := session.NewInMemoryRegistry()
	repo := ledger.NewMemoryRepository()
	factory := agent.NewDefaultFactory(stubProvider{}, &cognitive.ReAct{}, nil)
	eng, err := engine.New(registry, factory)
	require.NoError(t, err)

	be := &integrationTest{
		registry: registry,
		repo:     repo,
		eng:      eng,
	}

	c, err := xhttp.New(be, xhttp.WithoutUI())
	require.NoError(t, err)
	h, ok := c.(*xhttp.Handler)
	require.True(t, ok, "conduit must be a *Handler; got %T", c)

	// Bind a real listener so we can use httptest.NewUnstartedServer
	// with CloseClientConnections for clean test teardown.
	srv := httptest.NewUnstartedServer(h.ServeMux())
	srv.Start()

	cleanup := func() {
		srv.CloseClientConnections()
		srv.Close()
		_ = eng.Close(context.Background())
	}
	return srv, cleanup
}

// TestIntegration_FullStackCreateSubmitSSE exercises the canonical
// engine + HTTP conduit path: create a session, submit a user
// message, and verify that the SSE stream delivers the expected
// lifecycle events in order.
func TestIntegration_FullStackCreateSubmitSSE(t *testing.T) {
	srv, cleanup := startTestServer(t)
	defer cleanup()

	// Create a session via POST /sessions.
	createResp, err := http.Post(srv.URL+"/sessions", "application/json", nil)
	require.NoError(t, err)
	defer createResp.Body.Close()
	require.Equal(t, http.StatusCreated, createResp.StatusCode)
	var created map[string]string
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&created))
	id := created["id"]
	require.NotEmpty(t, id)

	// Open SSE in a goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sseReady := make(chan struct{})
	eventsCh := make(chan string, 8)
	errCh := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sessions/"+id+"/events", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			errCh <- &unexpectedStatusError{Status: resp.StatusCode}
			return
		}
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			if strings.HasPrefix(line, ":") {
				select {
				case <-sseReady:
				default:
					close(sseReady)
				}
			}
			if strings.HasPrefix(line, "data: ") {
				eventsCh <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// Wait for SSE handshake.
	select {
	case <-sseReady:
	case err := <-errCh:
		t.Fatalf("SSE setup failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("SSE handshake timed out")
	}

	// Submit a user message.
	body := strings.NewReader(`{"kind":"user_message","content":"hello"}`)
	submitResp, err := http.Post(srv.URL+"/sessions/"+id+"/events", "application/json", body)
	require.NoError(t, err)
	defer submitResp.Body.Close()
	require.Equal(t, http.StatusAccepted, submitResp.StatusCode)

	// Read events until lifecycle "done" arrives.
	deadline := time.After(3 * time.Second)
	sawTurnComplete := false
	sawDone := false
	for !sawDone {
		select {
		case payload := <-eventsCh:
			var evt map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(payload), &evt))
			kind, _ := evt["kind"].(string)
			switch kind {
			case "turn_complete":
				sawTurnComplete = true
			case "lifecycle":
				if phase, _ := evt["phase"].(string); phase == "done" {
					sawDone = true
				}
			}
		case err := <-errCh:
			t.Fatalf("SSE reader failed: %v", err)
		case <-deadline:
			t.Fatal("timed out waiting for lifecycle done event")
		}
	}
	assert.True(t, sawTurnComplete, "expected turn_complete event before lifecycle done")
}

// TestIntegration_DeleteSessionReturns404 confirms that a deleted
// session can no longer be looked up.
func TestIntegration_DeleteSessionReturns404(t *testing.T) {
	srv, cleanup := startTestServer(t)
	defer cleanup()

	createResp, err := http.Post(srv.URL+"/sessions", "application/json", nil)
	require.NoError(t, err)
	defer createResp.Body.Close()
	require.Equal(t, http.StatusCreated, createResp.StatusCode)
	var created map[string]string
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&created))
	id := created["id"]

	req, _ := http.NewRequest("DELETE", srv.URL+"/sessions/"+id, nil)
	delResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer delResp.Body.Close()
	require.Equal(t, http.StatusNoContent, delResp.StatusCode)

	// Subsequent operations on the deleted session should fail.
	getResp, err := http.Get(srv.URL + "/sessions/" + id)
	require.NoError(t, err)
	defer getResp.Body.Close()
	assert.Equal(t, http.StatusNotFound, getResp.StatusCode)
}

// unexpectedStatusError is a small helper for SSE handshake errors.
type unexpectedStatusError struct {
	Status int
}

func (e *unexpectedStatusError) Error() string {
	return "unexpected SSE status: " + http.StatusText(e.Status)
}
