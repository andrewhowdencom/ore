package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
	"go.opentelemetry.io/otel/trace/noop"
)

// --- mocks ---

// mockProvider is a test double implementing provider.Provider. It
// writes its canned artifacts to the result channel and returns the
// configured error.
type mockProvider struct {
	artifacts []artifact.Artifact
	err       error
	called    int32
}

var _ provider.Provider = (*mockProvider)(nil)

func (m *mockProvider) Invoke(ctx context.Context, _ ledger.State, _ models.Spec, ch chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	atomic.AddInt32(&m.called, 1)
	for _, a := range m.artifacts {
		select {
		case ch <- a:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

// recordingPattern is a test double implementing cognitive.Pattern.
// It counts Run calls, records the most recent state argument, and
// returns the configured state and error.
type recordingPattern struct {
	name string

	mu     sync.Mutex
	calls  int32
	lastSt ledger.State

	err error
	ret ledger.State
}

var _ cognitive.Pattern = (*recordingPattern)(nil)

func (p *recordingPattern) Run(_ context.Context, st ledger.State) (ledger.State, error) {
	atomic.AddInt32(&p.calls, 1)
	p.mu.Lock()
	p.lastSt = st
	p.mu.Unlock()
	if p.ret != nil {
		return p.ret, p.err
	}
	return st, p.err
}

func (p *recordingPattern) Name() string { return p.name }

// recordedSpan is one entry in the recordingTracer.
type recordedSpan struct {
	name       string
	attributes map[attribute.Key]attribute.Value
}

// recordingTracer implements trace.Tracer and records every Start
// call. It returns a noop.Span from Start so the agent's defer
// span.End() is a safe no-op.
type recordingTracer struct {
	embedded.Tracer

	mu      sync.Mutex
	started []recordedSpan
}

var _ trace.Tracer = (*recordingTracer)(nil)

func (t *recordingTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	cfg := trace.NewSpanStartConfig(opts...)
	attrs := make(map[attribute.Key]attribute.Value, len(cfg.Attributes()))
	for _, kv := range cfg.Attributes() {
		attrs[kv.Key] = kv.Value
	}
	t.mu.Lock()
	t.started = append(t.started, recordedSpan{name: name, attributes: attrs})
	t.mu.Unlock()
	return trace.ContextWithSpan(ctx, noop.Span{}), noop.Span{}
}

// --- tests ---

func TestNew_RequiresPattern(t *testing.T) {
	assert.PanicsWithValue(t, "agent.New: WithPattern is required", func() {
		New("missing-pattern")
	})
}

func TestNew_StoresOptions(t *testing.T) {
	pat := &recordingPattern{name: "test"}
	p := &mockProvider{}
	spec := models.Spec{Name: "test-model"}
	a := New("test",
		WithProvider(p),
		WithSpec(spec),
		WithPattern(pat),
	)
	defer a.Close()

	assert.Equal(t, "test", a.Name())
	assert.Same(t, p, a.Provider())
	assert.Equal(t, spec, a.Spec())
	assert.Same(t, pat, a.Pattern())
	require.NotNil(t, a.Step())
}

func TestNew_EmptySliceOptions(t *testing.T) {
	pat := &recordingPattern{name: "test"}
	a := New("test",
		WithProvider(&mockProvider{}),
		WithPattern(pat),
		WithTransforms(),
		WithHandlers(),
		WithInvokeOptions(),
	)
	defer a.Close()
	require.NotNil(t, a.Step())
}

func TestAgent_Run_DelegatesToPattern(t *testing.T) {
	pat := &recordingPattern{name: "test"}
	a := New("test",
		WithProvider(&mockProvider{}),
		WithPattern(pat),
	)
	defer a.Close()

	st := &ledger.Buffer{}
	result, err := a.Run(context.Background(), st)
	require.NoError(t, err)
	assert.Same(t, st, result)
	assert.Equal(t, int32(1), atomic.LoadInt32(&pat.calls))

	pat.mu.Lock()
	defer pat.mu.Unlock()
	assert.Same(t, st, pat.lastSt)
}

func TestAgent_Run_PatternErrorPropagates(t *testing.T) {
	want := errors.New("pattern failed")
	pat := &recordingPattern{name: "test", err: want}
	a := New("test",
		WithProvider(&mockProvider{}),
		WithPattern(pat),
	)
	defer a.Close()

	_, err := a.Run(context.Background(), &ledger.Buffer{})
	require.ErrorIs(t, err, want)
}

func TestAgent_Run_RecordsAgentRunSpan(t *testing.T) {
	tracer := &recordingTracer{}
	pat := &recordingPattern{name: "react"}
	a := New("test-agent",
		WithProvider(&mockProvider{}),
		WithPattern(pat),
		WithTracer(tracer),
	)
	defer a.Close()

	_, err := a.Run(context.Background(), &ledger.Buffer{})
	require.NoError(t, err)

	tracer.mu.Lock()
	defer tracer.mu.Unlock()
	require.Len(t, tracer.started, 1, "expected exactly one span start")
	got := tracer.started[0]
	assert.Equal(t, "agent.run", got.name)

	nameVal, ok := got.attributes["agent.name"]
	require.True(t, ok, "agent.name attribute missing")
	assert.Equal(t, "test-agent", nameVal.AsString())

	patVal, ok := got.attributes["agent.pattern"]
	require.True(t, ok, "agent.pattern attribute missing")
	assert.Equal(t, "react", patVal.AsString())
}

func TestAgent_Run_ReusesStep(t *testing.T) {
	pat := &recordingPattern{name: "test"}
	a := New("test",
		WithProvider(&mockProvider{}),
		WithPattern(pat),
	)
	defer a.Close()

	step1 := a.Step()
	_, err := a.Run(context.Background(), &ledger.Buffer{})
	require.NoError(t, err)
	_, err = a.Run(context.Background(), &ledger.Buffer{})
	require.NoError(t, err)
	step2 := a.Step()
	assert.Same(t, step1, step2, "step should be reused across Run calls")
	assert.Equal(t, int32(2), atomic.LoadInt32(&pat.calls))
}

func TestAgent_Subscribe_ReturnsChannel(t *testing.T) {
	a := New("test",
		WithProvider(&mockProvider{}),
		WithPattern(&recordingPattern{name: "test"}),
	)
	ch := a.Subscribe()
	require.NotNil(t, ch)

	require.NoError(t, a.Close())

	// The channel should be closed after Close.
	_, ok := <-ch
	assert.False(t, ok, "channel should be closed after agent Close")
}

func TestAgent_Close_Idempotent(t *testing.T) {
	a := New("test",
		WithProvider(&mockProvider{}),
		WithPattern(&recordingPattern{name: "test"}),
	)
	require.NoError(t, a.Close())
	require.NoError(t, a.Close(), "second Close should be a no-op")
}

func TestAgent_Accessors_PostClose(t *testing.T) {
	a := New("test",
		WithProvider(&mockProvider{}),
		WithPattern(&recordingPattern{name: "test"}),
	)
	// Capture accessors before Close.
	name := a.Name()
	step := a.Step()
	spec := a.Spec()
	pat := a.Pattern()
	prov := a.Provider()
	require.NoError(t, a.Close())

	// Accessors are pure reads; they should still work post-Close.
	assert.Equal(t, "test", name)
	assert.NotNil(t, step)
	assert.Equal(t, models.Spec{}, spec)
	assert.NotNil(t, pat)
	assert.NotNil(t, prov)
}
