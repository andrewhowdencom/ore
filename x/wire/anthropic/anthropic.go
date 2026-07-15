// Package anthropic implements a provider adapter for the Anthropic Messages
// API and the OpenRouter /api/v1/messages mirror. It wraps the official
// github.com/anthropics/anthropic-sdk-go client.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/provider/retry"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Provider implements provider.Provider for the Anthropic Messages API
// (Anthropic native and the OpenRouter /api/v1/messages mirror) using
// the official anthropic-sdk-go SDK.
//
// Like the openai module, the provider resolves host identity once at
// construction time so the read- and write-sides agree on auth header
// selection, base URL, and SDK options without re-walking the option
// list on every Invoke.
type Provider struct {
	client anthropic.Client
	tracer trace.Tracer
	// isOpenRouter is the resolved boolean used to choose the
	// OpenRouter branch (`Authorization: Bearer <key>`) over the
	// Anthropic native branch (`x-api-key: <key>`). Resolved at
	// construction from the configured base URL so the auth header
	// cannot drift between turns of the same Provider.
	isOpenRouter bool
	// nameResolver translates the canonical [models.Spec.Name]
	// into the wire name understood by the upstream host. Set from
	// [WithNameResolver] at construction; defaults to identity.
	// The resolver is invoked once per [Provider.Invoke] on the
	// effective model. See [WithNameResolver] for the extension
	// contract.
	nameResolver func(string) string
}

// WithTools returns an InvokeOption that configures the set of available tools
// for a single provider invocation. It delegates to the provider-agnostic
// provider.WithTools for cross-adapter compatibility.
func WithTools(tools []tool.Tool) provider.InvokeOption {
	return provider.WithTools(tools)
}

// temperatureOption is a per-invocation option that sets the sampling temperature
// for the Anthropic Messages API. Anthropic clamps the range to [0.0, 1.0];
// out-of-range values are forwarded as-is and the upstream rejects them.
type temperatureOption struct {
	t float64
}

func (temperatureOption) IsInvokeOption() {}

// WithTemperature returns an InvokeOption that sets the sampling temperature
// for a single provider invocation. The default of 1.0 applies when this
// option is not supplied.
func WithTemperature(t float64) provider.InvokeOption {
	return temperatureOption{t: t}
}

// maxTokensOption is a per-invocation option that sets the maximum number of
// tokens the model is permitted to generate. Anthropic requires max_tokens to
// be set on every request; the SDK will reject a request with max_tokens=0
// unless the caller intends to populate the prompt cache without generating
// a response, which is not a supported use case here.
type maxTokensOption struct {
	n int64
}

func (maxTokensOption) IsInvokeOption() {}

// WithMaxTokens returns an InvokeOption that sets the maximum number of
// tokens the model is permitted to generate on a single invocation.
//
// Callers are responsible for picking a value appropriate to the model
// and the task; different Anthropic models have different output caps,
// and a summary task typically warrants a larger budget than a
// short-form chat turn. There is no default — omitting this option
// leaves the Anthropic SDK / model default in effect, matching the
// openai adapter's behavior. Long-running callers (e.g. compaction
// strategies) should set this explicitly so the model has room to
// produce a complete response.
func WithMaxTokens(n int64) provider.InvokeOption {
	return maxTokensOption{n: n}
}

// thinkingLevelOption is a per-invocation option that sets the
// thinking effort level for the Anthropic Messages API. The level is
// translated to a thinking.budget_tokens value at request time as a
// percentage of max_tokens, with a 1024-token floor and a
// (max_tokens - 1024) ceiling. The empty level or "off" disables
// extended thinking entirely.
//
// Note: this option is retained for adapter-specific use cases that
// need to override the spec's ThinkingLevel on a per-call basis. The
// canonical way to set a thinking level is via the [models.Spec]
// passed to Invoke.
type thinkingLevelOption struct {
	level models.ThinkingLevel
}

func (thinkingLevelOption) IsInvokeOption() {}

// WithThinkingLevel returns an InvokeOption that sets the thinking
// effort level for a single provider invocation. The level is
// translated to a token budget at request time; see translateThinkingLevel
// for the per-level mapping and the floor/ceiling behavior. The empty
// level and models.ThinkingLevelOff are both treated as "no thinking".
func WithThinkingLevel(l models.ThinkingLevel) provider.InvokeOption {
	return thinkingLevelOption{level: l}
}

// thinkingLevelPercentages maps each non-off level to a fraction of
// max_tokens allocated to the thinking budget. The 1024 floor and the
// (max_tokens - 1024) ceiling are applied in translateThinkingLevel.
// These percentages are deliberately not absolute: the level is the
// user's intent, and the resulting budget scales with the model's
// output budget so "medium" feels similar across 8k, 32k, and 128k
// output models.
var thinkingLevelPercentages = map[models.ThinkingLevel]float64{
	models.ThinkingLevelMinimal: 0.02,
	models.ThinkingLevelLow:     0.08,
	models.ThinkingLevelMedium:  0.25,
	models.ThinkingLevelHigh:    0.50,
	models.ThinkingLevelMax:     0.80,
}

// anthropicMinThinkingBudget is the floor Anthropic enforces on
// thinking.budget_tokens. Any percentage that computes a smaller
// value is clamped up to this.
const anthropicMinThinkingBudget int64 = 1024

// anthropicMinVisibleResponse is the minimum number of tokens
// reserved for the visible response. The thinking budget is capped
// at (maxTokens - anthropicMinVisibleResponse) so the model always
// has at least this much room for the answer.
const anthropicMinVisibleResponse int64 = 1024

// translateThinkingLevel returns the thinking budget in tokens for
// the given level and max_tokens. The second return is false when the
// level is provider.ThinkingLevelOff or unset, in which case the
// request should omit the thinking field. Unknown levels are treated
// as off (forward compatibility with future level additions).
//
// The returned budget is at least anthropicMinThinkingBudget and at
// most (maxTokens - anthropicMinVisibleResponse). If maxTokens is
// smaller than anthropicMinThinkingBudget + anthropicMinVisibleResponse,
// the budget is clamped to 0 with a false return.
func translateThinkingLevel(l models.ThinkingLevel, maxTokens int64) (int64, bool) {
	if l == "" || l == models.ThinkingLevelOff {
		return 0, false
	}
	pct, ok := thinkingLevelPercentages[l]
	if !ok {
		// Unknown level: treat as off for forward compatibility.
		return 0, false
	}
	if maxTokens < anthropicMinThinkingBudget+anthropicMinVisibleResponse {
		// max_tokens is too small to even satisfy the 1024 floor
		// while leaving room for the visible response. Disable
		// thinking rather than send a request the upstream would
		// reject.
		return 0, false
	}
	budget := int64(pct * float64(maxTokens))
	if budget < anthropicMinThinkingBudget {
		budget = anthropicMinThinkingBudget
	}
	if ceiling := maxTokens - anthropicMinVisibleResponse; budget > ceiling {
		budget = ceiling
	}
	return budget, true
}

// invokeOptions is the resolved per-invocation configuration collected by
// Invoke. Each field has its own default so a missing option does not
// silently change behavior.
type invokeOptions struct {
	temperature      float64
	temperatureSet   bool
	maxTokens        int64
	maxTokensSet     bool
	thinkingLevel    models.ThinkingLevel
	thinkingLevelSet bool
	tools            []tool.Tool
	toolsSet         bool
}

// applyInvokeOptions walks the provider.InvokeOption list and folds it into
// an invokeOptions struct. Unknown option types are ignored; only the
// temperature / maxTokens / thinkingLevel / tools options are recognized
// here. This mirrors the openai module's pattern.
func applyInvokeOptions(opts ...provider.InvokeOption) invokeOptions {
	var out invokeOptions
	for _, opt := range opts {
		switch o := opt.(type) {
		case temperatureOption:
			out.temperature = o.t
			out.temperatureSet = true
		case maxTokensOption:
			out.maxTokens = o.n
			out.maxTokensSet = true
		case provider.MaxTokensOption:
			// Provider-agnostic form. N <= 0 is "no opinion";
			// the adapter does not set the wire field. Callers
			// that need an explicit output budget configure
			// models.Spec.MaxOutputTokens on the agent bundle
			// (or pass it through agent.WithSpec); the agent's
			// step forwards the spec to the provider, which
			// translates MaxOutputTokens into the wire format
			// here.
			if o.N > 0 {
				out.maxTokens = o.N
				out.maxTokensSet = true
			}
		case thinkingLevelOption:
			out.thinkingLevel = o.level
			out.thinkingLevelSet = true
		case provider.ToolsOption:
			// The carrier's Tools field is a function so the
			// tool list can be resolved dynamically from
			// (ctx, state). At this stage we resolve it
			// eagerly with a no-op context/state because the
			// list is static for the duration of the call;
			// the framework supplies the live values at
			// the call site, not here.
			out.tools = o.Tools(context.Background(), ledger.NewThread())
			out.toolsSet = true
		}
	}
	return out
}

// config holds the build-time configuration for the Provider.
type config struct {
	apiKey     string
	baseURL    string
	httpClient option.HTTPClient
	tracer     trace.Tracer
	// nameResolver translates the canonical [models.Spec.Name] into
	// the wire name understood by the upstream host. It is invoked
	// once per [Provider.Invoke] on the effective model. The default
	// is identity: the spec name is forwarded verbatim. The option
	// is the extension point for gateways (OpenRouter, Vercel,
	// etc.) that need to map canonical names to their own
	// vendor-specific identifiers.
	nameResolver func(string) string
}

// Option configures a Provider via the functional options pattern.
type Option func(*config)

// identityNameResolver is the default name resolver: it forwards
// the canonical spec name unchanged. Captured as a package-level
// var so callers can compare against the default via pointer
// equality when they need to detect "no resolver configured" at
// runtime.
var identityNameResolver = func(canonical string) string { return canonical }

// WithNameResolver sets a function that translates the canonical
// [models.Spec.Name] into the wire name understood by the upstream
// host. It is invoked once per Invoke on the effective model. The
// default is identity: the spec name is forwarded verbatim.
//
// Gateways (OpenRouter, Vercel, MiniMax, …) supply a resolver that
// maps the upstream-primary's name (e.g. "claude-opus-4-5") to the
// gateway's vendor-specific identifier. The resolver is also the
// extension point for any application that needs a custom mapping.
//
// A nil resolver is treated as identity. An empty canonical name
// is not passed to the resolver: the caller has already enforced
// spec.Name != "" before resolution, and a zero return is forwarded
// as the model name (the wire layer will reject the empty string
// if the host is strict about it).
func WithNameResolver(r func(canonical string) string) Option {
	return func(c *config) {
		if r == nil {
			c.nameResolver = identityNameResolver
			return
		}
		c.nameResolver = r
	}
}

// WithAPIKey sets the API key for the Anthropic-compatible provider. The key
// is interpreted as an Anthropic native key when the configured base URL is
// empty or targets api.anthropic.com; it is interpreted as an OpenRouter key
// when the configured base URL targets openrouter.ai.
func WithAPIKey(key string) Option {
	return func(c *config) {
		c.apiKey = key
	}
}

// WithBaseURL sets a custom API base URL. Use this to point at the OpenRouter
// /api/v1/messages mirror, a local proxy, or any other Anthropic-compatible
// host. The provider inspects the base URL to choose the auth header
// (`x-api-key` on Anthropic native, `Authorization: Bearer <key>` on
// OpenRouter).
func WithBaseURL(url string) Option {
	return func(c *config) {
		c.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client for the provider. This is primarily
// useful for testing; the SDK's default client is appropriate for production.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		if client == nil {
			return
		}
		c.httpClient = client
	}
}

// WithTracer configures an OpenTelemetry tracer for the provider. When set,
// Invoke opens a `provider.invoke` span with `SpanKindClient` and records
// the `model` and `thread_id` attributes. When unset, Invoke is a
// no-op for tracing.
func WithTracer(tracer trace.Tracer) Option {
	return func(c *config) {
		c.tracer = tracer
	}
}

// New creates an Anthropic-compatible provider. WithAPIKey is
// required; all other options are optional. The base URL is inspected
// once at construction time to decide which auth header to apply, so
// callers do not need to track host identity across invocations.
//
// The model identity is supplied per-call via the [models.Spec]
// argument to [Provider.Invoke]; there is no constructor option for
// it. The [WithNameResolver] option configures how the canonical
// spec name is translated to the wire name understood by the
// configured host. The default is identity: the spec name is
// forwarded unchanged.
func New(opts ...Option) (*Provider, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.apiKey == "" {
		return nil, fmt.Errorf("missing required option: apiKey")
	}

	// Default to identity resolution. WithNameResolver already
	// guards against nil resolvers, but the zero-value config
	// (no WithNameResolver supplied) still needs a default.
	if cfg.nameResolver == nil {
		cfg.nameResolver = identityNameResolver
	}

	sdkOpts := buildSDKOptions(cfg)

	return &Provider{
		client:       anthropic.NewClient(sdkOpts...),
		tracer:       cfg.tracer,
		isOpenRouter: isOpenRouter(cfg.baseURL),
		nameResolver: cfg.nameResolver,
	}, nil
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// Invoke serializes state into an Anthropic Messages streaming request
// via the SDK, walks the resulting SSE event stream, and emits canonical
// ore artifact types in wire-arrival order. Reasoning and text deltas
// are emitted directly; tool-call fragments are accumulated by the
// generic loop accumulator; signatures and redacted_thinking blocks are
// emitted as complete ReasoningSignature artifacts so the write-side
// can replay them on the next turn.
//
// The provider never closes the channel (the contract forbids it).
// Every send is guarded by `select { case ch <- ...: case <-ctx.Done():
// return ctx.Err() }` so context cancellation aborts the call promptly.
// Invoke serializes state into an Anthropic Messages streaming request
// via the SDK, walks the resulting SSE event stream, and emits canonical
// ore artifact types in wire-arrival order. Reasoning and text deltas
// are emitted directly; tool-call fragments are accumulated by the
// generic loop accumulator; signatures and redacted_thinking blocks are
// emitted as complete ReasoningSignature artifacts so the write-side
// can replay them on the next turn.
//
// The spec carries the model identity and inference configuration. The
// adapter translates spec fields to the Anthropic wire format:
// spec.Name is the model identifier, spec.Temperature is the sampling
// temperature, spec.ThinkingLevel is translated to a thinking budget,
// spec.MaxOutputTokens is mapped to max_tokens, and spec.StopSequences
// to the stop_sequences field.
//
// The provider never closes the channel (the contract forbids it).
// Every send is guarded by `select { case ch <- ...: case <-ctx.Done():
// return ctx.Err() }` so context cancellation aborts the call promptly.
func (p *Provider) Invoke(ctx context.Context, s ledger.State, spec models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	var span trace.Span
	if p.tracer != nil {
		ctx, span = p.tracer.Start(ctx, "provider.invoke", trace.WithSpanKind(trace.SpanKindClient))
		// The model attribute is set after the spec walk so it
		// reflects the *effective* (per-call) model. A trace
		// that lies about which model served a request makes
		// per-invocation switching impossible to debug.
		defer span.End()
		// Attach httptrace hooks to record granular HTTP
		// lifecycle events (DNS, connect, TLS, first-byte) on
		// the provider.invoke span. Only enabled when a tracer
		// is configured.
		ctx = httptrace.WithClientTrace(ctx, otelhttptrace.NewClientTrace(ctx, otelhttptrace.WithoutSubSpans()))
	}

	// The spec's Name is the only source of model identity. An empty
	// Name is a hard error: we cannot issue a request without knowing
	// which model to call.
	if spec.Name == "" {
		return fmt.Errorf("anthropic: spec.Name is empty; model identity is required")
	}

	serialized := p.serializeMessages(s)

	inv := applyInvokeOptions(opts...)

	// Spec fields take precedence over the per-call options above. A
	// spec field that is the zero value (empty Name, nil pointer, etc.)
	// leaves the corresponding value untouched. The spec is the
	// canonical source of truth for model identity and inference
	// configuration.
	if spec.Temperature != nil {
		inv.temperature = *spec.Temperature
		inv.temperatureSet = true
	}
	if spec.MaxOutputTokens > 0 {
		inv.maxTokens = spec.MaxOutputTokens
		inv.maxTokensSet = true
	}
	if spec.ThinkingLevel != "" {
		inv.thinkingLevel = spec.ThinkingLevel
		inv.thinkingLevelSet = true
	}
	effectiveModel := spec.Name
	if p.nameResolver != nil {
		effectiveModel = p.nameResolver(spec.Name)
	}

	if span != nil {
		span.SetAttributes(attribute.String("model", effectiveModel))
		if id, ok := loop.ThreadIDFrom(ctx); ok {
			span.SetAttributes(attribute.String("thread_id", id))
		}
	}

	// Emit one span event per tool_use block plus request-shape
	// counts on the provider.invoke span. No-op when no tracer is
	// configured. Must run after serializeMessages so we have the
	// SDK-typed content blocks to walk.
	summarizeToolUse(serialized.messages, span)

	params := anthropic.MessageNewParams{
		Model:    anthropic.Model(effectiveModel),
		Messages: serialized.messages,
		// MaxTokens is intentionally not set here. The Anthropic
		// SDK rejects a request with MaxTokens=0, so omitting
		// the field altogether leaves the SDK / model default
		// in effect. This matches the openai adapter's
		// "no default; caller must opt in" behavior, and
		// removes the previous 'fail loudly with a 1-token
		// response' default that produced silent garbage
		// (a markdown heading fragment, e.g. '##') when
		// callers forgot to set the option.
		//
		// If inv.maxTokensSet is true, the per-invocation value
		// is applied below.
	}
	if len(serialized.system) > 0 {
		params.System = serialized.system
	}
	if inv.maxTokensSet {
		params.MaxTokens = inv.maxTokens
	}
	if inv.temperatureSet {
		params.Temperature = anthropic.Float(inv.temperature)
	}
	if inv.toolsSet && len(inv.tools) > 0 {
		params.Tools = p.serializeTools(inv.tools)
	}
	if inv.thinkingLevelSet {
		if budget, ok := translateThinkingLevel(inv.thinkingLevel, inv.maxTokens); ok {
			params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budget)
		}
	}

	stream := p.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	// The SDK's MessageDeltaUsage is cumulative across the stream,
	// so we buffer the latest message_delta usage and emit a single
	// Usage artifact at message_stop. Emitting per message_delta
	// would double-count tokens.
	var pendingUsage *artifact.Usage
	var pendingStopReason artifact.StopReasonKind

	for stream.Next() {
		event := stream.Current()
		if err := p.dispatchEvent(ctx, event, ch, &pendingUsage, &pendingStopReason); err != nil {
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			return wrapForRetry(err)
		}
		if _, ok := event.AsAny().(anthropic.MessageStopEvent); ok {
			break
		}
	}

	if err := stream.Err(); err != nil {
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return fmt.Errorf("anthropic streaming: %w", wrapForRetry(err))
	}

	if pendingStopReason != "" {
		if err := sendOrCancel(ctx, ch, artifact.StopReason{Reason: pendingStopReason}); err != nil {
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			return wrapForRetry(err)
		}
	}
	if pendingUsage != nil {
		select {
		case ch <- *pendingUsage:
		case <-ctx.Done():
			if span != nil {
				span.RecordError(ctx.Err())
				span.SetStatus(codes.Error, ctx.Err().Error())
			}
			return ctx.Err()
		}
	}
	return nil
}

// dispatchEvent routes a single SSE event from the SDK to the right
// ore artifact emission. The dispatch is exhaustive over the SDK's
// typed event union; unknown variants are silently skipped so a future
// SDK addition does not break older providers.
//
// pendingUsage and pendingStopReason are out-parameters for the
// per-stream envelopes: the message_delta event carries both the
// cumulative usage block and the upstream stop_reason, and both are
// emitted on the channel once the stream closes (after message_stop).
// Carrying them through the dispatch loop avoids re-reading the SSE
// payload twice and keeps the SDK's cumulative-usage contract in one
// place.
func (p *Provider) dispatchEvent(
	ctx context.Context,
	event anthropic.MessageStreamEventUnion,
	ch chan<- artifact.Artifact,
	pendingUsage **artifact.Usage,
	pendingStopReason *artifact.StopReasonKind,
) error {
	switch ev := event.AsAny().(type) {
	case anthropic.MessageStartEvent:
		// Envelope; the message body is built incrementally from
		// the content_block_* events that follow.
		return nil
	case anthropic.ContentBlockStartEvent:
		return p.dispatchBlockStart(ctx, ev, ch)
	case anthropic.ContentBlockDeltaEvent:
		return p.dispatchBlockDelta(ctx, ev, ch)
	case anthropic.ContentBlockStopEvent:
		// Block boundary; the loop accumulator flushes any
		// accumulated deltas when a non-delta artifact arrives.
		return nil
	case anthropic.MessageDeltaEvent:
		bufferUsage(ev.Usage, pendingUsage)
		bufferStopReason(ev.Delta.StopReason, pendingStopReason)
		return nil
	case anthropic.MessageStopEvent:
		// Final envelope; the caller breaks out of the event
		// loop on this event and emits the buffered usage.
		return nil
	default:
		// Unknown variant (e.g., a future SDK addition). Skip
		// rather than error so a version skew does not break
		// a live stream.
		return nil
	}
}

// dispatchBlockStart handles a content_block_start event. Thinking and
// text blocks carry no payload at start (the deltas do the work);
// redacted_thinking blocks carry an opaque data blob that must be
// preserved verbatim; tool_use blocks carry the ID and name that seed
// the first ToolCallDelta.
func (p *Provider) dispatchBlockStart(
	ctx context.Context,
	ev anthropic.ContentBlockStartEvent,
	ch chan<- artifact.Artifact,
) error {
	switch b := ev.ContentBlock.AsAny().(type) {
	case anthropic.ThinkingBlock, anthropic.TextBlock:
		// No payload at start; deltas will follow.
		return nil
	case anthropic.RedactedThinkingBlock:
		if b.Data == "" {
			return nil
		}
		sig := artifact.ReasoningSignature{
			Provider: "anthropic",
			SubKind:  "redacted",
			Data:     b.Data,
		}
		return sendOrCancel(ctx, ch, sig)
	case anthropic.ToolUseBlock:
		delta := artifact.ToolCallDelta{
			Index: int(ev.Index),
			ID:    b.ID,
			Name:  b.Name,
		}
		return sendOrCancel(ctx, ch, delta)
	default:
		// Other block types (server_tool_use, web_search_tool_result,
		// container_upload, etc.) are not modeled by ore; skip.
		return nil
	}
}

// dispatchBlockDelta handles a content_block_delta event. The four
// modeled variants map to ReasoningDelta, TextDelta, ToolCallDelta,
// and ReasoningSignature respectively. Empty payloads are skipped so
// the loop accumulator does not see zero-content deltas.
func (p *Provider) dispatchBlockDelta(
	ctx context.Context,
	ev anthropic.ContentBlockDeltaEvent,
	ch chan<- artifact.Artifact,
) error {
	switch d := ev.Delta.AsAny().(type) {
	case anthropic.ThinkingDelta:
		if d.Thinking == "" {
			return nil
		}
		return sendOrCancel(ctx, ch, artifact.ReasoningDelta{Content: d.Thinking})
	case anthropic.SignatureDelta:
		if d.Signature == "" {
			return nil
		}
		sig := artifact.ReasoningSignature{
			Provider: "anthropic",
			SubKind:  "signature",
			Data:     d.Signature,
		}
		return sendOrCancel(ctx, ch, sig)
	case anthropic.TextDelta:
		if d.Text == "" {
			return nil
		}
		return sendOrCancel(ctx, ch, artifact.TextDelta{Content: d.Text})
	case anthropic.InputJSONDelta:
		if d.PartialJSON == "" {
			return nil
		}
		delta := artifact.ToolCallDelta{
			Index:     int(ev.Index),
			Arguments: d.PartialJSON,
		}
		return sendOrCancel(ctx, ch, delta)
	default:
		// citations_delta and unknown variants are not modeled.
		return nil
	}
}

// bufferUsage replaces the pending Usage pointer with one built from
// the SDK's cumulative message_delta usage. The caller emits the
// buffered Usage once, at message_stop, to avoid the double-count
// trap (the SDK reports cumulative token counts, not per-delta
// deltas, so emitting on every message_delta would multiply-count).
//
// ThinkingTokens is set to nil when the upstream provider omits
// `output_tokens_details` from the usage object entirely (e.g.,
// proxies that don't forward the field); otherwise it is set to a
// pointer to the reported count. encoding/json drops the nil case
// from the JSON payload via the `omitempty` tag on artifact.Usage.
// Detection uses the SDK's respjson.Field.Valid() metadata, which
// is the same mechanism the SDK uses internally to enforce
// `api:"required"`. This requires anthropic-sdk-go v1.50.0 or
// later; the package go.mod pins v1.50.0+.
func bufferUsage(usage anthropic.MessageDeltaUsage, pending **artifact.Usage) {
	*pending = &artifact.Usage{
		PromptTokens:     int(usage.InputTokens),
		CompletionTokens: int(usage.OutputTokens),
		TotalTokens:      int(usage.InputTokens + usage.OutputTokens),
		CacheReadTokens:  int(usage.CacheReadInputTokens),
		CacheWriteTokens: int(usage.CacheCreationInputTokens),
		ThinkingTokens:   thinkingTokensPtr(usage),
	}
}

// thinkingTokensPtr returns nil when the upstream usage block omits
// `output_tokens_details`, and a pointer to the count when the field
// is present (including zero). See bufferUsage for the rationale.
func thinkingTokensPtr(usage anthropic.MessageDeltaUsage) *int {
	if !usage.JSON.OutputTokensDetails.Valid() {
		return nil
	}
	n := int(usage.OutputTokensDetails.ThinkingTokens)
	return &n
}

// bufferStopReason translates the upstream stop_reason from a
// message_delta event into the canonical artifact.StopReasonKind
// and stores it in the pending out-parameter. The caller emits the
// buffered StopReason once, at message_stop, immediately before the
// buffered Usage. Like bufferUsage, this avoids the (smaller) trap
// of translating on every event when the SDK only sends the final
// stop_reason in the message_delta that precedes message_stop.
func bufferStopReason(reason anthropic.StopReason, pending *artifact.StopReasonKind) {
	*pending = translateStopReason(reason)
}

// translateStopReason normalizes an Anthropic-specific stop_reason
// value into the canonical artifact.StopReasonKind used across all
// adapters. The mapping mirrors the table documented in the package:
// end_turn → stop, max_tokens → length, tool_use → tool_use,
// refusal → refusal, and anything else (including stop_sequence and
// future SDK additions) → other. The empty string maps to "" (no
// reason buffered) so callers can distinguish "upstream reported no
// reason" from "upstream reported a known reason" — although in
// practice the SDK always reports a reason because the field is
// api:"required" on the wire.
func translateStopReason(s anthropic.StopReason) artifact.StopReasonKind {
	switch s {
	case "":
		return ""
	case anthropic.StopReasonEndTurn:
		return artifact.StopReasonStop
	case anthropic.StopReasonMaxTokens:
		return artifact.StopReasonLength
	case anthropic.StopReasonToolUse:
		return artifact.StopReasonToolUse
	case anthropic.StopReasonRefusal:
		return artifact.StopReasonRefusal
	default:
		// stop_sequence, pause_turn, and any future SDK additions
		// land here. The 'other' bucket is the forward-compat slot.
		return artifact.StopReasonOther
	}
}

// sendOrCancel emits an artifact on the channel, respecting context
// cancellation. The contract forbids the adapter from closing the
// channel, so the only way out of a send is via the ctx.Done() arm.
func sendOrCancel(ctx context.Context, ch chan<- artifact.Artifact, a artifact.Artifact) error {
	select {
	case ch <- a:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// serializeTools converts provider-agnostic tool.Tool definitions into
// the SDK's ToolUnionParam shape. The Schema field is a JSON Schema
// object; we extract `properties` and `required` (the two fields the
// Anthropic API requires on input_schema) and forward them verbatim.
// Tools that omit Schema still produce a valid (if permissive) tool
// definition because Type defaults to "object" in the SDK.
func (p *Provider) serializeTools(tools []tool.Tool) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, len(tools))
	for i, t := range tools {
		schema := anthropic.ToolInputSchemaParam{
			Type: "object",
		}
		if t.Schema != nil {
			if props, ok := t.Schema["properties"]; ok {
				schema.Properties = props
			}
			if req, ok := t.Schema["required"]; ok {
				if rs, ok := req.([]string); ok {
					schema.Required = rs
				} else if rsi, ok := req.([]any); ok {
					for _, r := range rsi {
						if s, ok := r.(string); ok {
							schema.Required = append(schema.Required, s)
						}
					}
				}
			}
		}
		out[i] = anthropic.ToolUnionParamOfTool(schema, t.Name)
	}
	return out
}

// isOpenRouter reports whether the configured base URL targets OpenRouter.
// Detection is a substring match; OpenRouter publishes only that domain
// (and its subdomains), so a stricter URL parser would add complexity
// without meaningfully reducing false positives. Mirrors the openai
// module's `isOpenRouter` heuristic.
func isOpenRouter(baseURL string) bool {
	return strings.Contains(baseURL, "openrouter.ai")
}

// buildSDKOptions assembles the SDK option list at construction time. The
// auth header is selected here (not per invocation) so it cannot drift
// between turns of the same Provider. The dispatch is:
//
//   - isOpenRouter(cfg.baseURL) -> option.WithAuthToken, which sets
//     Authorization: Bearer <key>. OpenRouter's /api/v1/messages mirror
//     accepts Bearer tokens, not x-api-key.
//   - everything else (including an empty base URL, which defaults to
//     api.anthropic.com) -> option.WithAPIKey, which sets
//     x-api-key: <key>. The SDK also injects anthropic-version:
//     2023-06-01 in its requestconfig middleware (see
//     internal/requestconfig/requestconfig.go in the SDK), so callers
//     do not need a WithAnthropicVersion knob.
func buildSDKOptions(cfg *config) []option.RequestOption {
	authOpt := option.RequestOption(option.WithAPIKey(cfg.apiKey))
	if isOpenRouter(cfg.baseURL) {
		authOpt = option.WithAuthToken(cfg.apiKey)
	}
	sdkOpts := []option.RequestOption{authOpt}
	if cfg.baseURL != "" {
		sdkOpts = append(sdkOpts, option.WithBaseURL(cfg.baseURL))
	}
	if cfg.httpClient != nil {
		sdkOpts = append(sdkOpts, option.WithHTTPClient(cfg.httpClient))
	}
	return sdkOpts
}

// serializeResult is the package of message blocks that
// serializeMessages produces. The first slice is the per-turn
// MessageParam list (the wire-level `messages` field); the second is the
// optional top-level system text. System turns are collected here rather
// than emitted as MessageParams because the Anthropic Messages API does
// not accept a `system` role on input messages — system instructions
// must be set via the request-level `system` field.
//
// Order is preserved: the first slice is in state-turn order, with
// non-system roles in their original positions and system content
// stripped (so a state with two consecutive system turns at the front
// collapses into the system field without producing empty user turns).
type serializeResult struct {
	messages []anthropic.MessageParam
	system   []anthropic.TextBlockParam
}

// serializeMessages converts ore state into the wire-level message blocks
// expected by the Anthropic Messages API. It is the read-side of the
// reasoning round-trip: assistant turns that contain Reasoning and
// ReasoningSignature artifacts are reconstructed as the corresponding
// `thinking` and `redacted_thinking` content blocks so the upstream model
// receives a continuous reasoning chain across turns.
//
// Cross-provider ReasoningSignature artifacts (e.g. an OpenAI encrypted
// signature in an assistant turn produced by the openai adapter) are
// silently dropped; the Anthropic wire has no slot for them, and the
// alternative — encoding them in a system instruction — would corrupt
// the model's view of the conversation.
//
// Tool-call and tool-result artifacts are mapped to the SDK's
// `tool_use` and `tool_result` block types. Tool results live in
// RoleTool turns and are forwarded as user-role messages with
// tool_result content blocks, which is the Anthropic-recommended
// representation for round-tripping tool execution.
func (p *Provider) serializeMessages(s ledger.State) serializeResult {
	turns := s.Turns()
	out := serializeResult{
		messages: make([]anthropic.MessageParam, 0, len(turns)),
	}

	for _, turn := range turns {
		switch turn.Role {
		case ledger.RoleSystem:
			// Anthropic has no system role on input messages.
			// Collect text and append to the request-level system
			// field below. Non-text artifacts in a system turn
			// are dropped: there is no canonical wire slot for
			// them and silently re-routing to user would
			// mislead the model.
			if txt, ok := onlyText(turn.Artifacts); ok {
				out.system = append(out.system, anthropic.TextBlockParam{
					Text: txt,
				})
			}
		case ledger.RoleUser:
			txt := concatText(turn.Artifacts)
			out.messages = append(out.messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(txt),
			))
		case ledger.RoleAssistant:
			// An assistant turn with no content blocks is dropped
			// from the wire entirely. See serializeAssistantTurn
			// for the contract: an empty artifact slice produces
			// a sentinel and the caller skips it, because the
			// SDK's `omitzero` JSON tag would otherwise produce
			// `{"role":"assistant"}` with no required content
			// field — rejected by every Anthropic-compatible
			// upstream (Anthropic native returns 400; minimax
			// returns 2013 "input json is empty").
			if msg, ok := serializeAssistantTurn(turn.Artifacts); ok {
				out.messages = append(out.messages, msg)
			}
		case ledger.RoleTool:
			// Tool results belong on the user side of an
			// assistant turn, in tool_result content blocks.
			// Concatenate multiple tool results into a single
			// user message so the upstream sees them in one
			// batch, matching the SDK's recommended
			// round-trip shape.
			var blocks []anthropic.ContentBlockParamUnion
			for _, art := range turn.Artifacts {
				tr, ok := art.(artifact.ToolResult)
				if !ok {
					continue
				}
				blocks = append(blocks, anthropic.NewToolResultBlock(
					tr.ToolCallID,
					tr.LLMString(),
					tr.IsError,
				))
			}
			if len(blocks) > 0 {
				out.messages = append(out.messages, anthropic.NewUserMessage(blocks...))
			}
		default:
			// Unknown role: degrade to a user turn so the
			// upstream at least sees the content. This is
			// defensive; ore's state package defines exactly
			// four roles.
			txt := concatText(turn.Artifacts)
			out.messages = append(out.messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(txt),
			))
		}
	}

	return out
}

// serializeAssistantTurn walks the artifacts of a single assistant turn
// and emits the matching content blocks. Text and ToolCall are
// straightforward; Reasoning and ReasoningSignature require coordinated
// handling so a thinking block's signature is applied to the immediately
// preceding thinking block (or to a fresh empty block if the signature
// arrives first, which is the only legal Anthropic replay shape).
//
// The second return value is false when the artifact slice produces no
// content blocks at all. The caller (serializeMessages) uses it to drop
// the turn from the wire entirely.
//
// Why drop, not emit-as-empty: the Anthropic SDK's MessageParam.Content
// is tagged `json:"content,omitzero" api:"required"`, so an empty
// slice serializes as `{"role":"assistant"}` with no content field.
// Anthropic-compatible upstreams reject this: Anthropic native returns
// 400 with `messages.N.content: Input should be a valid non-empty
// array`; the minimax mirror surfaces the same failure as error 2013,
// "invalid params: Syntax error no sources available, the input json
// is empty". Dropping is safe because an empty assistant turn by
// definition has no tool_uses, so no downstream tool_result depends on
// it. (The framework's earlier doc comment claimed an empty slice
// produced "a valid (if empty) assistant message"; that was true before
// the SDK adopted omitzero and is wrong now.)
//
// Empirically, this case arises when a stream ends after `message_start`
// but before any `content_block_*` events have been observed — a
// streaming disconnect, or a sub-process error after the envelope
// landed but before any block arrived — leaving stop_reason and usage
// on a turn with no content. Before this fix, the assistant message
// was emitted as `{"role":"assistant"}` and the entire request was
// rejected by the upstream. After this fix, the turn is dropped at the
// wire boundary and state is unchanged.
func serializeAssistantTurn(artifacts []artifact.Artifact) (anthropic.MessageParam, bool) {
	var blocks []anthropic.ContentBlockParamUnion

	// pendingThinking holds the most-recent thinking block whose
	// signature has not yet been filled in. When a ReasoningSignature
	// arrives, the signature is attached to the pending block (or to
	// a freshly-emitted empty one) and the pending slot is cleared.
	//
	// We track the index rather than the block itself because the
	// SDK exposes ThinkingBlock with both fields; the simpler design
	// is to mutate the last block in `blocks` in place.
	pendingThinking := -1

	flushPending := func() {
		// The pending thinking block, if any, has already been
		// emitted with whatever text was available at the time
		// of the Reasoning artifact. Its signature may be empty
		// (Anthropic's wire permits an empty signature on
		// replay); we leave it as-is and clear the slot.
		pendingThinking = -1
	}

	for _, art := range artifacts {
		switch a := art.(type) {
		case artifact.Text:
			// A text block closes any pending thinking block.
			flushPending()
			blocks = append(blocks, anthropic.NewTextBlock(a.Content))
		case artifact.ToolCall:
			// A tool_use block closes any pending thinking block.
			flushPending()
			blocks = append(blocks, anthropic.NewToolUseBlock(
				a.ID,
				parseToolArguments(a),
				a.Name,
			))
		case artifact.Reasoning:
			// Emit a thinking block with the accumulated text
			// and an empty signature. The signature will be
			// filled in by a subsequent ReasoningSignature of
			// SubKind="signature", or the block will be left
			// with an empty signature (which is a valid
			// Anthropic wire shape for replay).
			blocks = append(blocks, anthropic.NewThinkingBlock("", a.Content))
			pendingThinking = len(blocks) - 1
		case artifact.ReasoningSignature:
			switch {
			case a.Provider != "anthropic":
				// Cross-provider signatures (e.g. OpenAI
				// encrypted) have no slot on the Anthropic
				// wire. Drop them silently: re-encoding
				// them in any other field would mislead
				// the upstream model.
			case a.SubKind == "redacted":
				// A redacted_thinking block is
				// self-contained: it carries the encrypted
				// reasoning payload as `data` and does
				// not consume or attach to a pending
				// thinking block. Emit it directly.
				blocks = append(blocks, anthropic.NewRedactedThinkingBlock(a.Data))
				flushPending()
			case a.SubKind == "signature":
				if pendingThinking < 0 {
					// A signature with no preceding
					// thinking block. Anthropic permits
					// this as a standalone empty
					// thinking block; emit one so the
					// signature has a carrier. Without
					// this, the upstream would reject
					// the request because a thinking
					// signature cannot appear in
					// isolation.
					blocks = append(blocks, anthropic.NewThinkingBlock(a.Data, ""))
				} else {
					// Attach the signature to the
					// most-recent thinking block in
					// place. The thinking-block param
					// is a struct; we can reach into
					// it via the OfThinking field.
					tb := blocks[pendingThinking].OfThinking
					if tb != nil {
						tb.Signature = a.Data
					}
				}
				flushPending()
			}
		}
	}

	// If the turn ended with a pending thinking block whose signature
	// never arrived, leave the empty signature in place: Anthropic
	// accepts this on replay and the framework will continue to
	// collect signatures in subsequent turns if any arrive.

	if len(blocks) == 0 {
		return anthropic.MessageParam{}, false
	}
	return anthropic.NewAssistantMessage(blocks...), true
}

// parseToolArguments converts an artifact.ToolCall's argument payload
// into the form the SDK's tool_use block expects (`any`). The wire
// format is always derived from ToolCall.Arguments, the JSON object
// the model streamed; ToolCall.Display is intentionally not consulted
// (it is a human-rendering concern, not a wire-format concern).
//
// The lookup order is:
//
//   - Empty Arguments: return an empty object so the SDK produces a
//     well-formed `input: {}` rather than failing serialization.
//   - Arguments parses as JSON: forward the parsed value. The
//     upstream model is responsible for emitting a JSON object for
//     tool_use.input; if it emits something else, the API will
//     reject it with a wire-protocol error that names the offending
//     message — which is more useful than silently papering over a
//     schema violation at the framework layer.
//   - Arguments fails to parse: pass the raw string through so the
//     upstream can produce a useful error.
//
// This function deliberately does not consult ToolCall.Display. A
// previous implementation preferred Display over Arguments, which
// caused non-dict display values (e.g. string labels from tools
// whose DisplayHint returns a pre-formatted title) to be sent as
// the tool_use.input field. The Anthropic API requires input to be a
// JSON object, and the resulting 400 ("Input should be a valid
// dictionary (2013)") failed the entire request. See
// .plans/decouple-toolcall-display-from-wire.md for the full
// rationale.
func parseToolArguments(tc artifact.ToolCall) any {
	if tc.Arguments == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(tc.Arguments), &v); err == nil {
		return v
	}
	return tc.Arguments
}

// classifyToolUseInput names the shape of a `tool_use.input` value
// after it has been routed through parseToolArguments. The taxonomy is
// four values:
//
//   - "object":      the value is a non-string, non-nil value
//     (typically map[string]any from JSON). This is the only shape
//     the upstream API accepts and the happy path.
//   - "string":      the value is a literal string. At the wire layer
//     this is reachable only via parseToolArguments' fallback (raw
//     Arguments when JSON parsing fails), but historically also
//     surfaced when a tool.Tool.DisplayHint string leaked into the
//     input field — see parseToolArguments for the regression
//     history that produced "Input should be a valid dictionary".
//   - "null":        the value is nil or missing.
//   - "parse_error": sentinel; not produced by the current
//     parseToolArguments path, but retained so the four-kind
//     taxonomy round-trips with the upstream API error vocabulary.
//
// The classifier is intentionally non-numeric and does not surface
// the input value itself: when investigating an upstream failure of
// the form "messages.N.content.K.tool_use.input: Input should be a
// valid dictionary", knowing whether the wire payload was an object
// vs. something else is the entire question.
func classifyToolUseInput(v any) string {
	if v == nil {
		return "null"
	}
	if _, ok := v.(string); ok {
		return "string"
	}
	return "object"
}

// summarizeToolUse emits one span event per `tool_use` block in the
// serialized request, and two integer counts on the provider.invoke
// span. This is what makes a 400 of the form
// "messages.114.content.1.tool_use.input: Input should be a valid
// dictionary" diagnosable from the trace alone: the offending
// tool_use shows up as an `anthropic.tool_use` event with a
// non-"object" input_kind.
//
// Always-on when a tracer is configured via WithTracer; no separate
// opt-in. Behavior is unchanged when no tracer is configured — the
// helper short-circuits on a nil span.
//
// We surface structural metadata only — id, name, input shape — and
// never the argument values. This preserves the framework's
// "defaults to safe" property: a user pointing their OTel exporter
// at a remote backend will not exfiltrate filesystem paths, command
// lines, or API keys via this trace.
//
// TODO: redact known credential keys before surfacing values, if/when
// the input value is ever added to the event payload.
func summarizeToolUse(messages []anthropic.MessageParam, span trace.Span) {
	if span == nil {
		return
	}
	toolUseCount := 0
	for _, m := range messages {
		for _, b := range m.Content {
			if b.OfToolUse == nil {
				continue
			}
			toolUseCount++
			span.AddEvent("anthropic.tool_use", trace.WithAttributes(
				attribute.String("id", b.OfToolUse.ID),
				attribute.String("name", b.OfToolUse.Name),
				attribute.String("input_kind", classifyToolUseInput(b.OfToolUse.Input)),
			))
		}
	}
	span.SetAttributes(
		attribute.Int("anthropic.request.message_count", len(messages)),
		attribute.Int("anthropic.request.tool_use_count", toolUseCount),
	)
}

// concatText extracts and concatenates Text artifacts from a slice,
// separated by a single newline when more than one is present. This
// mirrors the openai adapter's behavior so the user-visible rendering
// of multi-Text user turns is consistent across providers.
func concatText(artifacts []artifact.Artifact) string {
	var b strings.Builder
	for i, art := range artifacts {
		t, ok := art.(artifact.Text)
		if !ok {
			continue
		}
		if i > 0 && b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(t.Content)
	}
	return b.String()
}

// onlyText returns the concatenated text of a turn if and only if the
// turn contains exactly one Text artifact and no other artifact types.
// This is the test that distinguishes a "pure system message" (which
// becomes a TextBlockParam on the request-level system field) from a
// "mixed-content" system turn (which is dropped to avoid losing
// non-text content silently). Mixed content in a system turn is a
// signal that the caller is constructing a malformed message; the
// conservative policy is to drop the turn rather than serialize an
// ambiguous shape to the upstream API.
func onlyText(artifacts []artifact.Artifact) (string, bool) {
	if len(artifacts) != 1 {
		return "", false
	}
	t, ok := artifacts[0].(artifact.Text)
	if !ok {
		return "", false
	}
	return t.Content, true
}

// retriableError wraps an *anthropic.Error so the retry decorator
// can recognise 5xx/429 responses via the retry.HTTPError
// interface without depending on the SDK directly. It preserves
// the original error chain via Unwrap, so existing code that does
// errors.As(err, &anthropic.Error{}) still works.
//
// The wrapped value is a pointer because apierror.Error's
// methods (including Error) are defined on the pointer receiver;
// a value of anthropic.Error does not implement the error
// interface.
type retriableError struct {
	inner *anthropic.Error
}

func (e *retriableError) Error() string {
	return e.inner.Error()
}

func (e *retriableError) StatusCode() int {
	return e.inner.StatusCode
}

func (e *retriableError) Header() http.Header {
	if e.inner.Response == nil {
		return nil
	}
	return e.inner.Response.Header
}

func (e *retriableError) Unwrap() error {
	return e.inner
}

// wrapForRetry converts an SDK error into one that implements
// retry.HTTPError. Non-HTTP errors (and errors that are not
// *anthropic.Error) are returned unchanged. The returned error
// preserves the original chain so that
// errors.As(err, &anthropic.Error{}) continues to resolve downstream.
func wrapForRetry(err error) error {
	var anthErr *anthropic.Error
	if errors.As(err, &anthErr) {
		return &retriableError{inner: anthErr}
	}
	return err
}

// Compile-time assertion that *retriableError satisfies retry.HTTPError.
var _ retry.HTTPError = (*retriableError)(nil)
