package agent_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"go.opentelemetry.io/otel/trace/noop"
)

type stubProvider struct{}

func (stubProvider) Invoke(ctx context.Context, s ledger.State, spec models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	return nil
}

type stubPattern struct {
	called atomic.Int32
}

func (s *stubPattern) Name() string { return "stub" }
func (s *stubPattern) Run(ctx context.Context, st ledger.State) (ledger.State, error) {
	s.called.Add(1)
	return st, nil
}

func newSession(t *testing.T, meta map[string]string) *session.Session {
	t.Helper()

	thread := ledger.NewThread()
	sess := session.New("test", thread)
	t.Cleanup(func() { _ = sess.Close() })

	for k, v := range meta {
		sess.SetMetadata(k, v)
	}
	return sess
}

func TestFactory_SpecFromMetadata(t *testing.T) {
	t.Parallel()

	sess := newSession(t, map[string]string{
		agent.MetadataKeyModelName:            "gpt-4",
		agent.MetadataKeyModelTemperature:     "0.7",
		agent.MetadataKeyModelMaxOutputTokens: "1024",
		agent.MetadataKeyModelThinkingLevel:   "high",
		"unrelated":                           "ignored",
	})

	pat := &stubPattern{}
	tracer := noop.NewTracerProvider().Tracer("test")
	f := agent.NewDefaultFactory(stubProvider{}, pat, tracer)

	ag, err := f.Build(sess)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ag == nil {
		t.Fatal("Build returned nil agent")
	}
	if ag.Name() != "test" {
		t.Errorf("agent.Name() = %q, want %q", ag.Name(), "test")
	}
}

func TestFactory_SpecFromMetadata_Empty(t *testing.T) {
	t.Parallel()

	sess := newSession(t, nil)

	pat := &stubPattern{}
	tracer := noop.NewTracerProvider().Tracer("test")
	f := agent.NewDefaultFactory(stubProvider{}, pat, tracer)

	ag, err := f.Build(sess)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ag == nil {
		t.Fatal("Build returned nil agent")
	}
	// With no metadata, the spec is the zero value. The agent is
	// constructed with an empty spec; the loop's default (if any)
	// would take over at runtime, but that's outside the factory's
	// responsibility.
}

func TestFactory_SpecFromMetadata_MalformedValues(t *testing.T) {
	t.Parallel()

	sess := newSession(t, map[string]string{
		agent.MetadataKeyModelName:            "claude-opus",
		agent.MetadataKeyModelTemperature:     "not-a-float",
		agent.MetadataKeyModelMaxOutputTokens: "not-an-int",
		agent.MetadataKeyModelThinkingLevel:   "high",
	})

	pat := &stubPattern{}
	tracer := noop.NewTracerProvider().Tracer("test")
	f := agent.NewDefaultFactory(stubProvider{}, pat, tracer)

	// Malformed values are silently ignored; only the well-formed
	// fields propagate to the agent's spec.
	ag, err := f.Build(sess)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ag == nil {
		t.Fatal("Build returned nil agent")
	}
}

func TestFactory_BuildPatternUsed(t *testing.T) {
	t.Parallel()

	sess := newSession(t, nil)
	pat := &stubPattern{}
	tracer := noop.NewTracerProvider().Tracer("test")
	f := agent.NewDefaultFactory(stubProvider{}, pat, tracer)

	ag, err := f.Build(sess)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ag == nil {
		t.Fatal("Build returned nil agent")
	}
	// Calling Run delegates to the pattern; this exercises that the
	// pattern (not just an internal stub) is wired through.
	_, _ = ag.Run(context.Background(), sess.Thread())
	if got := pat.called.Load(); got != 1 {
		t.Fatalf("pattern called %d times, want 1", got)
	}
}

func TestMetadataKeys_Stable(t *testing.T) {
	// The slash command in x/tool/set_model reads these constants
	// to know which keys to write under. They must remain stable.
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"ModelName", agent.MetadataKeyModelName, "ore.model.name"},
		{"ModelThinkingLevel", agent.MetadataKeyModelThinkingLevel, "ore.model.thinking_level"},
		{"ModelTemperature", agent.MetadataKeyModelTemperature, "ore.model.temperature"},
		{"ModelMaxOutputTokens", agent.MetadataKeyModelMaxOutputTokens, "ore.model.max_output_tokens"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

// Compile-time assertion that *agent.DefaultFactory implements agent.Factory.
var _ agent.Factory = (*agent.DefaultFactory)(nil)
var _ cognitive.Pattern = (*stubPattern)(nil)
