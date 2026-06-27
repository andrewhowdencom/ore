package stdio

import (
	"bytes"
	"context"
	"errors"
	"github.com/andrewhowdencom/ore/models"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (m *mockProvider) Invoke(ctx context.Context, s state.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	for _, art := range m.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

type blockingProvider struct{}

func (p *blockingProvider) Invoke(ctx context.Context, s state.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	<-ctx.Done()
	return ctx.Err()
}

type multiTurnProvider struct {
	invocations int
}

func (m *multiTurnProvider) Invoke(ctx context.Context, s state.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	m.invocations++
	switch m.invocations {
	case 1:
		ch <- artifact.TextDelta{Content: "First turn: "}
		ch <- artifact.TextDelta{Content: "hello"}
	case 2:
		ch <- artifact.TextDelta{Content: "Second turn: "}
		ch <- artifact.TextDelta{Content: "world"}
	}
	return nil
}

func simpleProcessor() junk.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, _ models.Spec) (state.State, error) {
		spec := models.Spec{Name: "test-model"}; _ = spec
		return step.Turn(ctx, st, spec, prov)
	}
}

func newManager(prov provider.Provider) *junk.Manager {
	store := junk.NewMemoryStore()
	return junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
}

func TestNew_NilManager(t *testing.T) {
	c, err := New(nil)
	require.Error(t, err)
	require.Nil(t, c)
}

func TestNew_Defaults(t *testing.T) {
	prov := &mockProvider{}
	mgr := newManager(prov)
	c, err := New(mgr)
	require.NoError(t, err)
	require.NotNil(t, c)

	s, ok := c.(*stdio)
	require.True(t, ok)
	require.NotNil(t, s.in)
	require.NotNil(t, s.out)
}

func TestNew_WithThreadID(t *testing.T) {
	prov := &mockProvider{}
	mgr := newManager(prov)
	c, err := New(mgr, WithThreadID("test-thread"))
	require.NoError(t, err)
	require.NotNil(t, c)

	s, ok := c.(*stdio)
	require.True(t, ok)
	require.Equal(t, "test-thread", s.threadID)
}

func TestNew_WithInput(t *testing.T) {
	prov := &mockProvider{}
	mgr := newManager(prov)
	in := bytes.NewBufferString("hello")
	c, err := New(mgr, WithInput(in))
	require.NoError(t, err)

	s, ok := c.(*stdio)
	require.True(t, ok)
	require.Equal(t, in, s.in)
}

func TestNew_WithOutput(t *testing.T) {
	prov := &mockProvider{}
	mgr := newManager(prov)
	out := &bytes.Buffer{}
	c, err := New(mgr, WithOutput(out))
	require.NoError(t, err)

	s, ok := c.(*stdio)
	require.True(t, ok)
	require.Equal(t, out, s.out)
}

func TestDescriptor(t *testing.T) {
	require.Equal(t, "stdio", Descriptor.Name)
	require.NotEmpty(t, Descriptor.Description)
	require.NotEmpty(t, Descriptor.Capabilities)
}

func TestStart_HappyPath(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello, "},
			artifact.TextDelta{Content: "world!"},
		},
	}
	mgr := newManager(prov)

	out := &bytes.Buffer{}
	in := bytes.NewBufferString("hi")
	c, err := New(mgr, WithInput(in), WithOutput(out))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	require.NoError(t, err)
	require.Contains(t, out.String(), "Hello, world!")
}

func TestStart_ReasoningBlocks(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Let me think..."},
			artifact.ReasoningDelta{Content: "thinking..."},
			artifact.TextDelta{Content: " Done!"},
		},
	}
	mgr := newManager(prov)

	out := &bytes.Buffer{}
	in := bytes.NewBufferString("question")
	c, err := New(mgr, WithInput(in), WithOutput(out))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	require.NoError(t, err)
	require.Contains(t, out.String(), "Let me think...")
	require.Contains(t, out.String(), "thinking...")
	require.Contains(t, out.String(), "Done!")
	require.Contains(t, out.String(), "```reasoning")
	require.Contains(t, out.String(), "```")
}

func TestStart_ErrorEvent(t *testing.T) {
	prov := &mockProvider{
		err: errors.New("provider failure"),
	}
	mgr := newManager(prov)

	out := &bytes.Buffer{}
	in := bytes.NewBufferString("test")
	c, err := New(mgr, WithInput(in), WithOutput(out))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	require.Error(t, err)
	require.Contains(t, out.String(), "error:")
	require.Contains(t, out.String(), "provider failure")
}

func TestStart_NoticeEvent(t *testing.T) {
	// Build a custom processor that emits a NoticeEvent in addition to
	// the normal provider-driven turn. NoticeEvents flow through the
	// junk.Stream's FanOut alongside the other output events.
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, _ models.Spec) (state.State, error) {
		spec := models.Spec{Name: "test-model"}
		st, err := step.Turn(ctx, st, spec, prov)
		if err != nil {
			return st, err
		}
		step.Emit(ctx, loop.NoticeEvent{
			Notice: loop.Notice{Content: `role "foo" not found`, Severity: loop.SeverityError},
			Ctx:    loop.WithProvenance(ctx, "stdio"),
		})
		return st, nil
	})

	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	in := bytes.NewBufferString("hi")
	c, err := New(mgr, WithInput(in), WithOutput(out), WithStderr(errBuf))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	require.NoError(t, err)
	// Notice goes to stderr, prefixed with the severity label.
	require.Contains(t, errBuf.String(), `Error: role "foo" not found`)
	// Assistant stream stays clean (no notice on stdout).
	require.NotContains(t, out.String(), "role \"foo\" not found")
	require.NotContains(t, out.String(), "Error:")
}

func TestStart_WithThreadID(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "attached"},
		},
	}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	thr, err := store.Create()
	require.NoError(t, err)

	out := &bytes.Buffer{}
	in := bytes.NewBufferString("hello")
	c, err := New(mgr, WithInput(in), WithOutput(out), WithThreadID(thr.ID))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	require.NoError(t, err)
	require.Contains(t, out.String(), "attached")
}

func TestStart_ProvenanceFiltering(t *testing.T) {
	store := junk.NewMemoryStore()
	foreignProcessor := func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, _ models.Spec) (state.State, error) {
		step.SetEventContext(loop.WithProvenance(context.Background(), "other"))
		spec := models.Spec{Name: "test-model"}; _ = spec
		return step.Turn(ctx, st, spec, prov)
	}
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "should be ignored"},
		},
	}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, foreignProcessor)

	out := &bytes.Buffer{}
	in := bytes.NewBufferString("test")
	c, err := New(mgr, WithInput(in), WithOutput(out))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	require.NoError(t, err)
	require.NotContains(t, out.String(), "should be ignored")
}

func TestStart_ContextCancellation(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &blockingProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())

	out := &bytes.Buffer{}
	in := bytes.NewBufferString("test")
	c, err := New(mgr, WithInput(in), WithOutput(out))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err = c.Start(ctx)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled"))
}

func TestStart_MultiTurnCapture(t *testing.T) {
	prov := &multiTurnProvider{}
	store := junk.NewMemoryStore()
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, _ models.Spec) (state.State, error) {
		spec := models.Spec{Name: "test-model"}
		st, err := step.Turn(ctx, st, spec, prov)
		if err != nil {
			return st, err
		}
		return step.Turn(ctx, st, spec, prov)
	})

	out := &bytes.Buffer{}
	in := bytes.NewBufferString("hi")
	c, err := New(mgr, WithInput(in), WithOutput(out))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.Start(ctx)
	require.NoError(t, err)
	require.Contains(t, out.String(), "First turn: hello")
	require.Contains(t, out.String(), "Second turn: world")
}
