// Package main is a reference HTTP chat application that wires a
// real engine.Engine, a session.Registry, and a ledger.MemoryRepository
// to the x/conduit/http conduit's Backend interface. The application
// owns the inference-driving machinery; the conduit translates HTTP
// bytes to Backend calls and renders session output as Server-Sent
// Events.
//
// This is the canonical integration test for the engine + HTTP
// conduit migration. It demonstrates:
//
//   - session creation (POST /sessions)
//   - session attach (POST /sessions with thread_id)
//   - event submission (POST /sessions/{id}/events)
//   - session-wide output streaming (GET /sessions/{id}/events)
//   - thread listing (GET /threads)
//
// Usage:
//
//	ANTHROPIC_API_KEY=... go run ./examples/http-chat
//
// Without API keys, the test wires a stub provider that emits a
// single text artifact per turn.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/engine"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	xhttp "github.com/andrewhowdencom/ore/x/conduit/http"
)

// stubProvider is a provider.Provider that emits a single text
// artifact per Invoke call. It is used in the integration test so
// the example runs without a real provider.
type stubProvider struct{}

func (stubProvider) Invoke(ctx context.Context, _ ledger.State, _ models.Spec, ch chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	select {
	case ch <- artifact.Text{Content: "stub response"}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Build a session registry, ledger repository, and agent
	//    factory. The agent is wired with a stub provider so the
	//    example runs offline.
	registry := session.NewInMemoryRegistry()
	repo := ledger.NewMemoryRepository()
	factory := agent.NewDefaultFactory(stubProvider{}, &cognitive.ReAct{}, nil)

	// 2. Construct the engine. The engine owns per-session
	//    serialization and inference execution.
	eng, err := engine.New(registry, factory)
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	// 3. Wire the Backend implementation that the HTTP conduit
	//    consumes. The Backend hides the engine + repo + registry
	//    from the conduit; the conduit only sees Backend methods.
	backend := &appBackend{
		registry: registry,
		repo:     repo,
		eng:      eng,
	}

	// 4. Construct and start the HTTP conduit.
	addr := ":0"
	if v := os.Getenv("HTTP_ADDR"); v != "" {
		addr = v
	}
	c, err := xhttp.New(backend, xhttp.WithAddr(addr))
	if err != nil {
		return fmt.Errorf("create http conduit: %w", err)
	}

	return c.Start(ctx)
}

// appBackend implements xhttp.Backend. It composes the session
// registry, ledger repository, and engine into the operations the
// HTTP conduit consumes.
type appBackend struct {
	mu       sync.Mutex
	registry session.Registry
	repo     ledger.Repository
	eng      *engine.Engine
}

func (b *appBackend) CreateSession(ctx context.Context, threadID string) (*session.Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var sess *session.Session
	if threadID == "" {
		sess = session.New("sess-"+strconv.FormatInt(time.Now().UnixNano(), 10), ledger.NewThread())
	} else {
		turns, currentTip, err := b.repo.HydrateThread(ctx, threadID)
		if err != nil {
			return nil, fmt.Errorf("hydrate thread %s: %w", threadID, err)
		}
		th := ledger.NewThread()
		for _, t := range turns {
			th.SaveTurn(t)
		}
		th.SetCurrentTip(currentTip)
		sess = session.New(threadID, th)
	}

	if err := b.registry.Register(sess); err != nil {
		return nil, fmt.Errorf("register session: %w", err)
	}
	return sess, nil
}

func (b *appBackend) GetSession(ctx context.Context, id string) (*session.Session, error) {
	return b.registry.Get(id)
}

func (b *appBackend) Submit(ctx context.Context, id string, event session.Event) error {
	return b.eng.Submit(ctx, id, event)
}

func (b *appBackend) ListThreads(ctx context.Context) ([]xhttp.ThreadSummary, error) {
	ids, err := b.repo.ListThreadIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list thread IDs: %w", err)
	}
	out := make([]xhttp.ThreadSummary, 0, len(ids))
	for _, id := range ids {
		turns, _, err := b.repo.HydrateThread(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("hydrate thread %s: %w", id, err)
		}
		var lastAt time.Time
		var preview string
		for _, t := range turns {
			if t.Timestamp.After(lastAt) {
				lastAt = t.Timestamp
			}
			if preview == "" && t.Role == ledger.RoleUser {
				for _, art := range t.Artifacts {
					if text, ok := art.(artifact.Text); ok {
						preview = text.Content
						if len(preview) > 120 {
							preview = preview[:120] + "..."
						}
						break
					}
				}
			}
		}
		out = append(out, xhttp.ThreadSummary{
			ID:      id,
			Preview: preview,
			LastAt:  lastAt,
		})
	}
	return out, nil
}

func (b *appBackend) DeleteSession(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, err := b.registry.Remove(id)
	if err != nil {
		return err
	}
	_ = s.Close()
	return nil
}

// Compile-time interface assertion.
var _ xhttp.Backend = (*appBackend)(nil)
