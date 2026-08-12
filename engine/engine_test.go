package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"

	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
	"go.opentelemetry.io/otel/trace/noop"
)

// --- mocks ---

// noopProvider is a provider that emits no artifacts and never errors.
type noopProvider struct{}

var _ provider.Provider = (*noopProvider)(nil)

func (noopProvider) Invoke(_ context.Context, _ ledger.State, _ models.Spec, _ chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	return nil
}

// echoPattern runs the supplied state unchanged. Useful for verifying
// agent invocation without driving real inference.
type echoPattern struct{}

func (echoPattern) Name() string { return "echo" }
func (echoPattern) Run(_ context.Context, st ledger.State) (ledger.State, error) { return st, nil }

// factory constructs an agent.DefaultFactory.
func newFactory(t *testing.T) agent.Factory {
	t.Helper()
	return agent.NewDefaultFactory(noopProvider{}, echoPattern{}, noop.NewTracerProvider().Tracer("test"))
}

// drain reads from ch for up to max events or d, returning what it
// observed. Returns the events and whether the channel was closed.
func drain(ch <-chan loop.OutputEvent, d time.Duration, max int) ([]loop.OutputEvent, bool) {
	deadline := time.After(d)
	got := make([]loop.OutputEvent, 0, max)
	for i := 0; i < max; i++ {
		select {
		case evt, ok := <-ch:
			if !ok {
				return got, true
			}
			got = append(got, evt)
		case <-deadline:
			return got, false
		}
	}
	return got, false
}

// --- tests ---

func TestEngine_New_ValidatesDeps(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, newFactory(t)); err == nil {
		t.Fatal("New(nil, factory) returned nil error; expected validation failure")
	}
	if _, err := New(session.NewInMemoryRegistry(), nil); err == nil {
		t.Fatal("New(registry, nil) returned nil error; expected validation failure")
	}
}

func TestEngine_Submit_UnknownSessionReturnsErrSessionNotFound(t *testing.T) {
	t.Parallel()

	e, err := New(session.NewInMemoryRegistry(), newFactory(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = e.Close(context.Background()) })

	err = e.Submit(context.Background(), "missing", session.UserMessageEvent{Content: "hi"})
	if err == nil {
		t.Fatal("Submit returned nil; expected ErrSessionNotFound")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("error %v does not wrap ErrSessionNotFound", err)
	}
}

func TestEngine_Submit_DrainsUserMessageLifecycle(t *testing.T) {
	t.Parallel()

	reg := session.NewInMemoryRegistry()
	sess := session.New("alpha", ledger.NewThread())

	if err := reg.Register(sess); err != nil {
		t.Fatalf("Register: %v", err)
	}

	e, err := New(reg, newFactory(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Subscribe to lifecycle events before submitting so we can
	// observe the terminal "done" event.
	sub := sess.Subscribe("lifecycle", "error", "properties", "turn_complete")

	// IMPORTANT: register sess.Close LAST so it runs FIRST (LIFO). The
	// subscription channel closes when the session's step is closed; the
	// drain in the sub cleanup below blocks until that happens.
	t.Cleanup(func() {
		for range sub {
		}
	})
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	t.Cleanup(func() { _ = sess.Close() })

	if err := e.Submit(context.Background(), "alpha", session.UserMessageEvent{Content: "hello"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Expect: TurnCompleteEvent for the user turn + LifecycleEvent
	// "done" for the lifecycle marker.
	got, closed := drain(sub, time.Second, 4)
	if !closed && len(got) < 2 {
		t.Fatalf("expected at least 2 events, got %d (closed=%v): %+v", len(got), closed, got)
	}

	// Find the "done" lifecycle event.
	sawDone := false
	for _, evt := range got {
		if le, ok := evt.(loop.LifecycleEvent); ok && le.Phase == "done" {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatalf("expected LifecycleEvent{Phase: \"done\"}, got %+v", got)
	}

	// The session's thread must now contain the user turn (auto-append).
	turns := sess.Turns()
	if len(turns) != 1 {
		t.Fatalf("expected exactly 1 turn in the session thread, got %d", len(turns))
	}
	if turns[0].Role != ledger.RoleUser {
		t.Errorf("expected role=user, got %q", turns[0].Role)
	}
}

func TestEngine_Submit_InterruptSkipsInference(t *testing.T) {
	t.Parallel()

	reg := session.NewInMemoryRegistry()
	sess := session.New("alpha", ledger.NewThread())

	if err := reg.Register(sess); err != nil {
		t.Fatalf("Register: %v", err)
	}

	e, err := New(reg, newFactory(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sub := sess.Subscribe("lifecycle", "error", "properties", "turn_complete")

	// LIFO order: sub drain first, then e.Close, then sess.Close.
	t.Cleanup(func() {
		for range sub {
		}
	})
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	t.Cleanup(func() { _ = sess.Close() })

	if err := e.Submit(context.Background(), "alpha", session.InterruptEvent{Ctx: context.Background()}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	got, closed := drain(sub, time.Second, 4)
	if !closed && len(got) < 1 {
		t.Fatalf("expected at least 1 event, got %d (closed=%v): %+v", len(got), closed, got)
	}

	sawDone := false
	for _, evt := range got {
		if le, ok := evt.(loop.LifecycleEvent); ok && le.Phase == "done" {
			sawDone = true
		}
	}
	if !sawDone {
		t.Errorf("expected LifecycleEvent{Phase: \"done\"} after interrupt, got %+v", got)
	}

	// Interrupt must not add a turn to the thread.
	if turns := sess.Turns(); len(turns) != 0 {
		t.Errorf("interrupt should not append a turn; thread has %d turns", len(turns))
	}
}

func TestEngine_Submit_QueueFullReturnsErrQueueFull(t *testing.T) {
	t.Parallel()

	reg := session.NewInMemoryRegistry()
	sess := session.New("alpha", ledger.NewThread())
	t.Cleanup(func() { _ = sess.Close() })
	if err := reg.Register(sess); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Block the worker on a gate so the mailbox stays full. Queue
	// size 1: the first submission is dequeued by the worker, the
	// second fills the queue, the third must hit ErrQueueFull.
	gate := make(chan struct{})
	slow := &slowPattern{gate: gate}

	e, err := New(
		reg,
		agent.NewDefaultFactory(noopProvider{}, slow, noop.NewTracerProvider().Tracer("test")),
		WithQueueSize(1),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
		_ = e.Close(context.Background())
	})

	// First submission: dequeued by the worker, which blocks on gate.
	if err := e.Submit(context.Background(), "alpha", session.UserMessageEvent{Content: "first"}); err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	// Give the worker a moment to dequeue the first event.
	time.Sleep(50 * time.Millisecond)

	// Second submission: lands in the mailbox (capacity 1).
	if err := e.Submit(context.Background(), "alpha", session.UserMessageEvent{Content: "second"}); err != nil {
		t.Fatalf("second Submit: %v", err)
	}

	// Third submission: the worker is still blocked on gate, the
	// queue is at capacity, so this must return ErrQueueFull.
	err = e.Submit(context.Background(), "alpha", session.UserMessageEvent{Content: "third"})
	if err == nil {
		t.Fatal("third Submit returned nil; expected ErrQueueFull")
	}
	if !errors.Is(err, ErrQueueFull) {
		t.Errorf("error %v does not wrap ErrQueueFull", err)
	}
}

func TestEngine_Submit_FactoryErrorSurfacesErrorEvent(t *testing.T) {
	t.Parallel()

	reg := session.NewInMemoryRegistry()
	sess := session.New("alpha", ledger.NewThread())

	if err := reg.Register(sess); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Factory that always errors.
	failingFactory := failingFactory{err: errors.New("factory boom")}

	e, err := New(reg, failingFactory)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sub := sess.Subscribe("error", "lifecycle")

	// LIFO order: sub drain first, then e.Close, then sess.Close.
	t.Cleanup(func() {
		for range sub {
		}
	})
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	t.Cleanup(func() { _ = sess.Close() })

	if err := e.Submit(context.Background(), "alpha", session.UserMessageEvent{Content: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Expect: ErrorEvent + LifecycleEvent "done".
	got, closed := drain(sub, time.Second, 4)
	if !closed && len(got) < 2 {
		t.Fatalf("expected at least 2 events, got %d (closed=%v): %+v", len(got), closed, got)
	}

	sawError := false
	sawDone := false
	for _, evt := range got {
		switch e := evt.(type) {
		case loop.ErrorEvent:
			sawError = true
			// The factory error is wrapped in engine-side context.
			if e.Err == nil || e.Err.Error() != "agent factory: factory boom" {
				t.Errorf("expected error %q, got %v", "agent factory: factory boom", e.Err)
			}
		case loop.LifecycleEvent:
			if e.Phase == "done" {
				sawDone = true
			}
		}
	}
	if !sawError {
		t.Errorf("expected ErrorEvent, got %+v", got)
	}
	if !sawDone {
		t.Errorf("expected LifecycleEvent{Phase: \"done\"} after factory error, got %+v", got)
	}
}

func TestEngine_Close_RefusesFurtherSubmissions(t *testing.T) {
	t.Parallel()

	reg := session.NewInMemoryRegistry()
	sess := session.New("alpha", ledger.NewThread())
	t.Cleanup(func() { _ = sess.Close() })
	if err := reg.Register(sess); err != nil {
		t.Fatalf("Register: %v", err)
	}

	e, err := New(reg, newFactory(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = e.Submit(context.Background(), "alpha", session.UserMessageEvent{Content: "hi"})
	if err == nil {
		t.Fatal("Submit after Close returned nil; expected ErrClosed")
	}
	if !errors.Is(err, ErrClosed) {
		t.Errorf("error %v does not wrap ErrClosed", err)
	}
}

func TestEngine_Close_IsIdempotent(t *testing.T) {
	t.Parallel()

	e, err := New(session.NewInMemoryRegistry(), newFactory(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestEngine_ConcurrentSubmitsAcrossSessionsAreSafe(t *testing.T) {
	t.Parallel()

	reg := session.NewInMemoryRegistry()
	const numSessions = 4
	for i := 0; i < numSessions; i++ {
		s := session.New(fmt.Sprintf("s-%d", i), ledger.NewThread())
		t.Cleanup(func() { _ = s.Close() })
		if err := reg.Register(s); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	e, err := New(reg, newFactory(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = e.Close(context.Background()) })

	const perSession = 5
	done := make(chan struct{}, numSessions)
	for i := 0; i < numSessions; i++ {
		go func(id string) {
			for j := 0; j < perSession; j++ {
				if err := e.Submit(context.Background(), id, session.UserMessageEvent{Content: "msg"}); err != nil {
					t.Errorf("Submit(%s): %v", id, err)
				}
			}
			done <- struct{}{}
		}(fmt.Sprintf("s-%d", i))
	}

	for i := 0; i < numSessions; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("concurrent submits did not complete in time")
		}
	}

	// Give workers a moment to drain; every session's thread should
	// now contain exactly perSession user turns.
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < numSessions; i++ {
		s, err := reg.Get(fmt.Sprintf("s-%d", i))
		if err != nil {
			t.Fatalf("Get s-%d: %v", i, err)
		}
		if got := len(s.Turns()); got != perSession {
			t.Errorf("session s-%d has %d turns, want %d", i, got, perSession)
		}
	}
}

// --- helpers used by tests above ---

// slowPattern blocks on gate until the gate channel is closed, then
// returns the state unchanged.
type slowPattern struct {
	gate  chan struct{}
	calls atomic.Int32
}

func (s *slowPattern) Name() string { return "slow" }
func (s *slowPattern) Run(_ context.Context, st ledger.State) (ledger.State, error) {
	s.calls.Add(1)
	<-s.gate
	return st, nil
}

// failingFactory returns an error from Build.
type failingFactory struct {
	err error
}

func (f failingFactory) Build(_ *session.Session) (*agent.Agent, error) {
	return nil, f.err
}

// turnPattern invokes the underlying step's Turn once with the configured
// provider and spec. Unlike echoPattern (which just returns the state
// unchanged), this exercises Step.startSpan and lets the engine's wiring
// be observed end-to-end on the loop.turn span.
type turnPattern struct {
	name string
	step loop.TurnRunner
	spec models.Spec
	prov provider.Provider
}

var _ cognitive.Pattern = (*turnPattern)(nil)

func (p *turnPattern) Name() string { return p.name }
func (p *turnPattern) Run(ctx context.Context, st ledger.State) (ledger.State, error) {
	return p.step.Turn(ctx, st, p.spec, p.prov)
}
func (p *turnPattern) SetRuntime(step loop.TurnRunner, prov provider.Provider, spec models.Spec, _ trace.Tracer) {
	p.step = step
	p.prov = prov
	p.spec = spec
}

// recordingTracer captures every span Start and every SetAttributes
// call so the engine test can inspect the loop.turn span's final
// attribute set. Local copy of the loop package's recorder; the
// engine cannot import a _test.go file from another package.
type recordingTracer struct {
	embedded.Tracer

	mu      sync.Mutex
	started []startedRecording
}

type startedRecording struct {
	name       string
	attributes map[attribute.Key]attribute.Value
}

func (t *recordingTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	cfg := trace.NewSpanStartConfig(opts...)
	startAttrs := make(map[attribute.Key]attribute.Value, len(cfg.Attributes()))
	for _, kv := range cfg.Attributes() {
		startAttrs[kv.Key] = kv.Value
	}
	s := &recordingSpan{attrs: startAttrs}
	t.mu.Lock()
	t.started = append(t.started, startedRecording{name: name, attributes: s.attrs})
	t.mu.Unlock()
	return trace.ContextWithSpan(ctx, s), s
}

func (t *recordingTracer) snapshot() []startedRecording {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]startedRecording, len(t.started))
	copy(out, t.started)
	return out
}

type recordingSpan struct {
	noop.Span

	mu    sync.Mutex
	attrs map[attribute.Key]attribute.Value
}

func (s *recordingSpan) SetAttributes(kv ...attribute.KeyValue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attrs == nil {
		s.attrs = make(map[attribute.Key]attribute.Value, len(kv))
	}
	for _, k := range kv {
		s.attrs[k.Key] = k.Value
	}
}

// Compile-time assertions: the test helpers satisfy the interfaces the
// engine depends on.
var (
	_ agent.Factory    = failingFactory{}
	_ cognitive.Pattern = (*slowPattern)(nil)
	_ cognitive.Pattern = (*turnPattern)(nil)
)

func TestEngine_HandleEvent_AttachesSessionAttributesToTurnSpan(t *testing.T) {
	t.Parallel()

	reg := session.NewInMemoryRegistry()
	sess := session.New("alpha", ledger.NewThread())
	sess.SetMetadata("ore.model.name", "claude-opus-4-5")
	sess.SetMetadata("ore.model.thinking_level", "high")
	t.Cleanup(func() { _ = sess.Close() })
	if err := reg.Register(sess); err != nil {
		t.Fatalf("Register: %v", err)
	}

	tracer := &recordingTracer{}
	pat := &turnPattern{name: "turn"}
	factory := agent.NewDefaultFactory(noopProvider{}, pat, tracer)

	e, err := New(reg, factory)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sub := sess.Subscribe("lifecycle")

	// IMPORTANT: register sess.Close LAST so it runs FIRST (LIFO). The
	// subscription channel closes when the session's step is closed; the
	// drain in the sub cleanup below blocks until that happens.
	t.Cleanup(func() {
		for range sub {
		}
	})
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	t.Cleanup(func() { _ = sess.Close() })

	if err := e.Submit(context.Background(), "alpha", session.UserMessageEvent{Content: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Wait for the engine's terminal lifecycle marker.
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case evt := <-sub:
			if le, ok := evt.(loop.LifecycleEvent); ok && le.Phase == "done" {
				break loop
			}
		case <-deadline:
			t.Fatal("timed out waiting for lifecycle done")
		}
	}

	// The recorder should have observed at least one loop.turn
	// span emitted by the agent's internal step. Its attributes
	// must include the session.* entries set above.
	started := tracer.snapshot()
	var turn *startedRecording
	for i := range started {
		if started[i].name == "loop.turn" {
			turn = &started[i]
			break
		}
	}
	if turn == nil {
		var names []string
		for _, s := range started {
			names = append(names, s.name)
		}
		t.Fatalf("expected loop.turn span; recorded span names = %v", names)
	}

	attrs := turn.attributes
	if v, ok := attrs[attribute.Key("session.ore.model.name")]; !ok || v.AsString() != "claude-opus-4-5" {
		t.Errorf(`attrs["session.ore.model.name"] = %q (present=%v), want "claude-opus-4-5"`, v.AsString(), ok)
	}
	if v, ok := attrs[attribute.Key("session.ore.model.thinking_level")]; !ok || v.AsString() != "high" {
		t.Errorf(`attrs["session.ore.model.thinking_level"] = %q (present=%v), want "high"`, v.AsString(), ok)
	}
}
