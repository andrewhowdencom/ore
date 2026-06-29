package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Transform modifies the state view presented to the provider during
// inference. Implementations must not mutate the underlying persistent
// buffer; they may return a derived ledger.State wrapper instead.
//
// Multiple transforms compose in registration order. Each transform
// receives the state returned by the previous one. An error from any
// transform aborts the turn before the provider is invoked.
type Transform interface {
	Transform(ctx context.Context, st ledger.State) (ledger.State, error)
}

// OutputEvent represents any event emitted by a Step.
// All output events carry a context.Context so subscribers can access
// routing metadata uniformly. Events include wrapped artifacts
// (ArtifactEvent), turn completions (TurnCompleteEvent), and errors
// (ErrorEvent).
type OutputEvent interface {
	Kind() string
	Context() context.Context
}

// TurnCompleteEvent is emitted when a turn (assistant, user, system, or
// tool) has been fully constructed. OnEmit callbacks fire synchronously
// before the event reaches the async FanOut and may mutate persistent
// state; handlers run after OnEmit completes.
type TurnCompleteEvent struct {
	Turn ledger.Turn

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e TurnCompleteEvent) Kind() string { return "turn_complete" }

// Context returns the event context.
func (e TurnCompleteEvent) Context() context.Context { return e.Ctx }

// MarshalJSON serializes the event to JSON.
func (e TurnCompleteEvent) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind    string                 `json:"kind"`
		Turn    ledger.Turn             `json:"turn"`
		Context map[string]interface{} `json:"context,omitempty"`
	}
	o := output{
		Kind: "turn_complete",
		Turn: e.Turn,
	}
	if ctx := marshalEventContext(e.Ctx); ctx != nil {
		o.Context = ctx
	}
	return json.Marshal(o)
}

// ErrorEvent is emitted when a turn fails due to a provider or handler error.
type ErrorEvent struct {
	Err error

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e ErrorEvent) Kind() string { return "error" }

// Context returns the event context.
func (e ErrorEvent) Context() context.Context { return e.Ctx }

// MarshalJSON serializes the event to JSON.
func (e ErrorEvent) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind    string                 `json:"kind"`
		Message string                 `json:"message"`
		Context map[string]interface{} `json:"context,omitempty"`
	}
	o := output{
		Kind:    "error",
		Message: e.Err.Error(),
	}
	if ctx := marshalEventContext(e.Ctx); ctx != nil {
		o.Context = ctx
	}
	return json.Marshal(o)
}

// ActivityEvent signals that long-running operational work is happening.
// It is orthogonal to the inference lifecycle and is used for slash commands,
// tool execution, or any other background work that conduits should surface.
type ActivityEvent struct {
	Active      bool
	Description string

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e ActivityEvent) Kind() string { return "activity" }

// Context returns the event context.
func (e ActivityEvent) Context() context.Context { return e.Ctx }

// MarshalJSON serializes the event to JSON.
func (e ActivityEvent) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind        string                 `json:"kind"`
		Active      bool                   `json:"active"`
		Description string                 `json:"description"`
		Context     map[string]interface{} `json:"context,omitempty"`
	}
	o := output{
		Kind:        "activity",
		Active:      e.Active,
		Description: e.Description,
	}
	if ctx := marshalEventContext(e.Ctx); ctx != nil {
		o.Context = ctx
	}
	return json.Marshal(o)
}

// LifecycleEvent is emitted at structural boundaries of a single inference
// turn to signal phase transitions. Phases are linear per-pipeline:
//   - "submitted": the user message has been accepted and the provider call
//     is about to start (after transforms).
//   - "streaming": the first artifact has arrived from the provider.
type LifecycleEvent struct {
	Phase string // "submitted", "streaming"

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e LifecycleEvent) Kind() string { return "lifecycle" }

// Context returns the event context.
func (e LifecycleEvent) Context() context.Context { return e.Ctx }

// MarshalJSON serializes the event to JSON.
func (e LifecycleEvent) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind    string                 `json:"kind"`
		Phase   string                 `json:"phase"`
		Context map[string]interface{} `json:"context,omitempty"`
	}
	o := output{
		Kind:  "lifecycle",
		Phase: e.Phase,
	}
	if ctx := marshalEventContext(e.Ctx); ctx != nil {
		o.Context = ctx
	}
	return json.Marshal(o)
}

// ArtifactEvent wraps an artifact.Artifact with a context.Context so it
// can be emitted as an OutputEvent without polluting the artifact type
// with routing metadata.
type ArtifactEvent struct {
	Artifact artifact.Artifact

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx context.Context
}

// Kind returns the underlying artifact's kind.
func (e ArtifactEvent) Kind() string { return e.Artifact.Kind() }

// Context returns the event context.
func (e ArtifactEvent) Context() context.Context { return e.Ctx }

// MarshalJSON serializes the event to JSON. It merges the artifact's JSON
// with an optional context envelope.
func (e ArtifactEvent) MarshalJSON() ([]byte, error) {
	artData, err := json.Marshal(e.Artifact)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(artData, &m); err != nil {
		return nil, err
	}
	if ctx := marshalEventContext(e.Ctx); ctx != nil {
		m["context"] = ctx
	}
	return json.Marshal(m)
}

// PropertiesEvent carries ambient, persistent metadata as a map of
// key-value pairs. It is emitted by any producer holding a
// *junk.Stream and flows through the per-session FanOut so all
// conduits receive it simultaneously.
type PropertiesEvent struct {
	Properties map[string]string

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e PropertiesEvent) Kind() string { return "properties" }

// Context returns the event context.
func (e PropertiesEvent) Context() context.Context { return e.Ctx }

// MarshalJSON serializes the event to JSON.
func (e PropertiesEvent) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind       string                 `json:"kind"`
		Properties map[string]string      `json:"properties"`
		Context    map[string]interface{} `json:"context,omitempty"`
	}
	o := output{
		Kind:       "properties",
		Properties: e.Properties,
	}
	if ctx := marshalEventContext(e.Ctx); ctx != nil {
		o.Context = ctx
	}
	return json.Marshal(o)
}

// marshalEventContext extracts provenance and traceparent from a context and
// returns them as a map for JSON serialization. Returns nil if the context
// carries no provenance.
func marshalEventContext(ctx context.Context) map[string]interface{} {
	if ctx == nil {
		return nil
	}
	prov, ok := ProvenanceFrom(ctx)
	if !ok || prov == "" {
		return nil
	}
	result := map[string]interface{}{
		"provenance": prov,
	}
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		carrier := propagation.MapCarrier{}
		propagator := propagation.TraceContext{}
		propagator.Inject(ctx, carrier)
		if tp := carrier.Get("traceparent"); tp != "" {
			result["traceparent"] = tp
		}
	}
	return result
}

// outputEventEnvelope wraps an OutputEvent with an acknowledgment channel.
// The producer blocks until the FanOut closes done after delivering the event.
type outputEventEnvelope struct {
	event OutputEvent
	done  chan struct{}
}

// provenanceKey is the typed context key for provenance metadata.
type provenanceKey struct{}

// WithProvenance attaches a provenance name to the context.
func WithProvenance(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, provenanceKey{}, name)
}

// ProvenanceFrom extracts the provenance name from a context, if present.
func ProvenanceFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	name, ok := ctx.Value(provenanceKey{}).(string)
	return name, ok
}

// threadIDKey is the typed context key for thread identity.
type threadIDKey struct{}

// WithThreadID attaches a thread ID to the context.
func WithThreadID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, threadIDKey{}, id)
}

// ThreadIDFrom extracts the thread ID from a context, if present.
func ThreadIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(threadIDKey{}).(string)
	return id, ok
}

// OnEmit is a synchronous callback invoked by Emit before the event is
// forwarded to the async FanOut. OnEmit callbacks are blocking, ordered,
// and zero-drop. They replace previous direct ledger.Append calls,
// ensuring lossless state updates while keeping the event stream
// observable for UI conduits. This is the canonical mechanism for wiring
// state persistence.
type OnEmit func(ctx context.Context, event OutputEvent)

// Step executes a single complete inference turn: it invokes the provider,
// distributes streaming artifacts to subscribers via an embedded EventBus, and
// runs registered artifact handlers synchronously on the complete response.
type Step struct {
	eventBus     *EventBus
	pipeline     *Pipeline
	eventContext context.Context
	tracer       trace.Tracer
	// defaultSpec is the ModelSpec to use when Turn() is called
	// with an empty Spec. Per-call Spec overrides the default.
	defaultSpec models.Spec
}

// New creates a Step with the given options.
func New(opts ...Option) *Step {
	eb := newEventBus()
	pipe := newPipeline()
	s := &Step{
		eventBus: eb,
		pipeline: pipe,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Emit runs all registered OnEmit callbacks synchronously, then sends the
// event to the FanOut and blocks until it has been delivered.
// When a state has been bound via WithState, only TurnCompleteEvent is
// automatically appended to that state before OnEmit callbacks run. Other
// event types are passed through unchanged.
func (s *Step) Emit(ctx context.Context, event OutputEvent) {
	s.eventBus.Emit(ctx, event)
}

// Subscribe returns a receive-only channel of OutputEvents whose Kind()
// matches any of the given kinds. The channel is closed when the Step's
// FanOut is closed. Events are delivered non-blocking; slow subscribers
// may drop events.
func (s *Step) Subscribe(kinds ...string) <-chan OutputEvent {
	return s.eventBus.Subscribe(kinds...)
}

// SetEventContext sets the context.Context that will be attached to all
// subsequent output events emitted by this Step. It is used by
// Stream.Process to thread context from the input event through the
// turn pipeline. Callers must ensure this is called before Turn or
// Submit and cleared after (typically via defer).
func (s *Step) SetEventContext(ctx context.Context) {
	if ctx != nil {
		s.eventContext = context.WithoutCancel(ctx)
	} else {
		s.eventContext = nil
	}
}

// clearEventContext resets the context.Context on the Step to nil.
// It is the counterpart to SetEventContext and is invoked via
// defer in Turn and Submit to prevent context leakage between calls.
func (s *Step) clearEventContext() {
	s.eventContext = nil
}

// Close stops the Step's FanOut and closes all subscriber channels.
func (s *Step) Close() error {
	return s.eventBus.Close()
}

// Option configures a Step.
type Option func(*Step)

// WithTransforms configures inference assembly transforms that run
// before each provider call in Turn(). Transforms receive the state
// after any user/system/tool submissions and before the provider
// serializes it. They must not mutate the underlying buffer.
func WithTransforms(transforms ...Transform) Option {
	return func(s *Step) {
		s.pipeline.transforms = append(s.pipeline.transforms, transforms...)
	}
}

// WithHandlers configures artifact handlers to run after each turn.
func WithHandlers(handlers ...Handler) Option {
	return func(s *Step) {
		s.pipeline.handlers = append(s.pipeline.handlers, handlers...)
	}
}

// WithOnEmit configures synchronous callbacks that run before the FanOut.
// OnEmit callbacks receive every OutputEvent emitted by the Step, including
// TurnCompleteEvent, ArtifactEvent, ErrorEvent, and LifecycleEvent.
// They are invoked in registration order, blocking, and zero-drop.
// This is the single place to wire state persistence, replacing previous
// patterns that mutated state directly inside Turn().
func WithOnEmit(fns ...OnEmit) Option {
	return func(s *Step) {
		s.eventBus.onEmit = append(s.eventBus.onEmit, fns...)
	}
}

// WithState binds a mutable ledger.State to the Step so that every
// TurnCompleteEvent emitted by the Step (including from finalizeTurn and
// from artifact handlers) is automatically appended to the state before
// OnEmit callbacks run. The supplied state is mutated in-place. When no
// state is bound, TurnCompleteEvent has no automatic side effects on ledger.
//
// WithState is the canonical mechanism for state persistence. Use
// WithOnEmit only for custom side-effects that do not also append to the
// same state, or duplicate turns will result.
func WithState(st ledger.State) Option {
	return func(s *Step) {
		s.eventBus.bound = st
	}
}

// WithInvokeOptions configures pre-bound provider invocation options that are
// automatically passed to every provider call made by this Step.
func WithInvokeOptions(opts ...provider.InvokeOption) Option {
	return func(s *Step) {
		s.pipeline.invokeOpts = append(s.pipeline.invokeOpts, opts...)
	}
}

// WithDefaultSpec configures the default [models.Spec] used by [Step.Turn]
// when the per-call Spec is empty. A non-empty per-call Spec always wins;
// this is the loop-level fallback (e.g. an application-wide default model
// that the session may override per-call).
func WithDefaultSpec(spec models.Spec) Option {
	return func(s *Step) {
		s.defaultSpec = spec
	}
}

// WithTracer configures an OpenTelemetry tracer for the Step.
// When configured, Turn and Submit create spans for each inference turn.
func WithTracer(tracer trace.Tracer) Option {
	return func(s *Step) {
		s.tracer = tracer
	}
}

// startSpan creates a "loop.turn" internal span when a tracer is configured.
// It reads thread_id from the context and adds it as a span attribute.
// Returns the span-attached context and a function that must be called to end
// the span. If no tracer is configured, returns the input context and a no-op.
func (s *Step) startSpan(ctx context.Context) (context.Context, func()) {
	if s.tracer == nil {
		return ctx, func() {}
	}
	ctx, span := s.tracer.Start(ctx, "loop.turn", trace.WithSpanKind(trace.SpanKindInternal))
	if id, ok := ThreadIDFrom(ctx); ok {
		span.SetAttributes(attribute.String("thread_id", id))
	}
	return ctx, func() { span.End() }
}

// Turn performs one inference turn with the given provider.
// The provider emits artifacts to a channel; all artifacts are forwarded to
// the Step's EventBus subscribers immediately as they arrive. Deltas implementing
// Accumulable are merged into blocks keyed by AccumulatorKey, so non-adjacent
// deltas of the same kind are combined into a single block. Accumulated blocks
// are flushed on non-delta boundaries and at stream end. The accumulated turn
// is appended to state once the provider returns. After the turn completes,
// all registered handlers are invoked on each artifact from the assistant turn.
// The operation is fully synchronous and blocking.
//
// The spec carries the model identity and inference configuration. A
// per-call spec takes precedence over the loop's default spec
// (configured via WithDefaultSpec); an empty per-call spec falls
// back to the default. The resolved spec is forwarded to the
// provider's Invoke.
func (s *Step) Turn(ctx context.Context, st ledger.State, spec models.Spec, p provider.Provider, opts ...provider.InvokeOption) (ledger.State, error) {
	defer s.clearEventContext()
	ctx, endSpan := s.startSpan(ctx)
	defer endSpan()

	// Per-call spec wins over the configured default.
	if spec.Name == "" && (spec.Window == 0 && spec.MaxOutputTokens == 0 &&
		spec.Temperature == nil && spec.ThinkingLevel == "" && spec.TopP == nil &&
		spec.TopK == nil && spec.Seed == nil && len(spec.StopSequences) == 0 &&
		spec.FrequencyPenalty == nil && spec.PresencePenalty == nil) {
		spec = s.defaultSpec
	}

	s.Emit(ctx, LifecycleEvent{Phase: "submitted", Ctx: s.eventContext})

	var hasStreamed bool
	st, accumulatedArtifacts, err := s.pipeline.Turn(ctx, st, spec, p, func(art artifact.Artifact) {
		if !hasStreamed {
			hasStreamed = true
			s.Emit(ctx, LifecycleEvent{Phase: "streaming", Ctx: s.eventContext})
		}
		s.Emit(ctx, ArtifactEvent{Artifact: art, Ctx: s.eventContext})
	}, opts...)

	if err != nil {
		if !errors.Is(err, context.Canceled) {
			s.Emit(context.Background(), ErrorEvent{Err: err, Ctx: s.eventContext})
		}
		return st, fmt.Errorf("turn failed: %w", err)
	}

	turn := ledger.Turn{Role: ledger.RoleAssistant, Artifacts: accumulatedArtifacts, Timestamp: time.Now()}
	s.Emit(ctx, TurnCompleteEvent{Turn: turn, Ctx: s.eventContext})

	if err := s.pipeline.RunHandlers(ctx, accumulatedArtifacts, s); err != nil {
		return st, err
	}

	return st, nil
}

// applyDisplayHints scans accumulated artifacts for ToolCall values
// and, when a matching DisplayHint is found in the ToolsOption of the
// invocation options, parses the JSON arguments, runs the hint, and
// attaches the result to ToolCall.Display. It mutates the slice
// in-place.
//
// The hint's return value is purely a human-rendering artifact: it is
// read by MarkdownString and by display-layer conduits (TUI, log
// viewers, exporters) and is never consulted by any provider's
// wire-format code path. The wire format is always derived from
// ToolCall.Arguments, the JSON the model streamed. This separation is
// load-bearing — see artifact.ToolCall for the rationale.
func applyDisplayHints(ctx context.Context, artifacts []artifact.Artifact, opts []provider.InvokeOption) {
	hints := make(map[string]func(map[string]any) any)
	for _, opt := range opts {
		if to, ok := opt.(provider.ToolsOption); ok {
			tools := to.Tools(ctx, nil)
			for _, t := range tools {
				if t.DisplayHint != nil {
					hints[t.Name] = t.DisplayHint
				}
			}
		}
	}
	if len(hints) == 0 {
		return
	}
	for i, art := range artifacts {
		tc, ok := art.(artifact.ToolCall)
		if !ok {
			continue
		}
		hint, ok := hints[tc.Name]
		if !ok {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
			continue
		}
		if v := hint(args); v != nil {
			tc.Display = v
			artifacts[i] = tc
		}
	}
}

// Submit records a non-inference turn into state, runs registered handlers,
// and emits a TurnCompleteEvent to all subscribers. It is the canonical
// mechanism for user, system, or tool turns to enter the same artifact stream
// as assistant responses from Turn().
func (s *Step) Submit(ctx context.Context, st ledger.State, role ledger.Role, artifacts ...artifact.Artifact) (ledger.State, error) {
	ctx, endSpan := s.startSpan(ctx)
	defer endSpan()
	return s.finalizeTurn(ctx, st, role, artifacts)
}

// finalizeTurn builds a turn and emits a TurnCompleteEvent. OnEmit
// callbacks execute synchronously, in registration order, before
// handler processing and before the event is forwarded to the
// asynchronous FanOut. They may append to ledger. After OnEmit
// completes, registered handlers run on each artifact. It is the
// shared post-processing pipeline used by both Turn() and Submit().
func (s *Step) finalizeTurn(ctx context.Context, st ledger.State, role ledger.Role, artifacts []artifact.Artifact) (ledger.State, error) {
	turn := ledger.Turn{Role: role, Artifacts: artifacts, Timestamp: time.Now()}
	s.Emit(ctx, TurnCompleteEvent{Turn: turn, Ctx: s.eventContext})

	if err := s.pipeline.RunHandlers(ctx, artifacts, s); err != nil {
		return st, err
	}

	return st, nil
}
