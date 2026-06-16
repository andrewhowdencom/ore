// Package openai implements a provider adapter for OpenAI-compatible chat
// completions APIs. It wraps the official github.com/openai/openai-go client.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptrace"
	"strconv"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Provider implements provider.Provider for OpenAI-compatible APIs using the
// official OpenAI Go SDK.
type Provider struct {
	client openai.Client
	tracer trace.Tracer
	// includeReasoning is the resolved decision about whether to opt into
	// upstream reasoning traces via `reasoning: {include: true}` in the
	// request body. Resolved once at construction from the explicit
	// override and the base-URL heuristic so Invoke doesn't re-walk options.
	includeReasoning bool
	// isOpenRouter is the resolved boolean used by the write-side
	// reasoning-replay mutation to choose the OpenRouter branch (which
	// emits `reasoning_details[]` on each assistant message) over the
	// OpenAI-native branch (which concatenates reasoning into
	// `reasoning_content`). Resolved at construction from the same
	// signal that drives includeReasoning, so the read- and write-sides
	// are guaranteed to agree.
	isOpenRouter bool
}

// WithTools returns an InvokeOption that configures the set of available tools
// for a single provider invocation. It delegates to the provider-agnostic
// provider.WithTools for cross-adapter compatibility.
func WithTools(tools []tool.Tool) provider.InvokeOption {
	return provider.WithTools(tools)
}

// temperatureOption is a per-invocation option that sets the sampling temperature.
type temperatureOption struct {
	t float64
}

func (temperatureOption) IsInvokeOption() {}

// WithTemperature returns an InvokeOption that sets the sampling temperature
// for a single provider invocation.
func WithTemperature(t float64) provider.InvokeOption {
	return temperatureOption{t: t}
}

// thinkingLevelOption is a per-invocation option that sets the thinking
// effort level for OpenAI-compatible providers. The level is translated
// to OpenAI's reasoning_effort field at request time. The "off" level
// omits the field entirely.
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
// effort level for a single provider invocation. The level is mapped
// to OpenAI's reasoning_effort vocabulary (low | medium | high);
// levels outside that vocabulary are clamped (minimal -> low; max ->
// high) because OpenAI's vocabulary is smaller than the framework's.
// models.ThinkingLevelOff and the empty level both disable reasoning.
func WithThinkingLevel(l models.ThinkingLevel) provider.InvokeOption {
	return thinkingLevelOption{level: l}
}

// translateThinkingLevel returns OpenAI's reasoning_effort string for
// the given level, or the empty string if the request should omit the
// field. Unknown levels return the empty string (treated as "off") for
// forward compatibility.
func translateThinkingLevel(l models.ThinkingLevel) string {
	switch l {
	case models.ThinkingLevelMinimal, models.ThinkingLevelLow:
		return "low"
	case models.ThinkingLevelMedium:
		return "medium"
	case models.ThinkingLevelHigh, models.ThinkingLevelMax:
		return "high"
	}
	// Off, empty, and unknown all disable reasoning.
	return ""
}

// WithReasoningInclude returns an Option that explicitly sets whether the
// provider should opt into receiving reasoning traces via the
// `reasoning.include` request body field. This is the OpenRouter extension
// that controls whether the upstream model forwards `reasoning_content` in
// its SSE stream. When this option is set, it overrides any default
// auto-detection based on the configured base URL. Pass `true` to force
// inclusion (e.g., on OpenRouter-compatible hosts that are not detected by
// the default heuristic), or `false` to suppress inclusion even on
// OpenRouter.
func WithReasoningInclude(include bool) Option {
	return func(c *config) {
		c.reasoningInclude = &include
	}
}

// isOpenRouter reports whether the configured base URL targets OpenRouter.
// Detection is intentionally a substring match because the provider
// publishes only that domain (and its subdomains), so a stricter URL
// parser would add complexity without meaningfully reducing false
// positives.
func isOpenRouter(baseURL string) bool {
	return strings.Contains(baseURL, "openrouter.ai")
}

// wantsReasoningInclude resolves the final decision about whether to inject
// `reasoning: {include: true}` into the request body. The explicit override
// wins when set, including the `false` value (the escape hatch to suppress
// the field even on OpenRouter). When the override is unset, the decision
// falls back to base-URL auto-detection.
func wantsReasoningInclude(cfg *config) bool {
	if cfg.reasoningInclude != nil {
		return *cfg.reasoningInclude
	}
	return isOpenRouter(cfg.baseURL)
}

// maxTokensOption is a per-invocation option that sets the maximum number of
// tokens the model may generate in the response.
type maxTokensOption struct {
	n int64
}

func (maxTokensOption) IsInvokeOption() {}

// WithMaxTokens returns an InvokeOption that sets the max_tokens parameter for
// a single provider invocation. This controls the maximum number of tokens the
// model will generate in its response, independent of the context window size.
func WithMaxTokens(n int64) provider.InvokeOption {
	return maxTokensOption{n: n}
}

// sessionIDOption is a per-invocation option that sets a stable session
// identifier used by the host for prefix-cache affinity. On OpenAI native
// this maps to the prompt_cache_key request field; on OpenRouter /
// Anthropic-via-OpenRouter it is informational (Anthropic-style cache_control
// blocks are the actual cache primitive on those hosts, and the session id
// is only useful as a stable key if the host chooses to honor it).
type sessionIDOption struct {
	id string
}

func (sessionIDOption) IsInvokeOption() {}

// WithSessionID returns an InvokeOption that sets the prompt_cache_key on
// outgoing chat completion requests. A stable session id allows OpenAI
// native to route subsequent requests to the same prefix cache. On other
// hosts the field is either a no-op or informational; the value is always
// safe to set.
func WithSessionID(id string) provider.InvokeOption {
	return sessionIDOption{id: id}
}

// cacheControlOption is a per-invocation option that opts into Anthropic-style
// cache_control blocks on the request. The presence of the option (it is a
// zero-sized type) is the signal; the value is irrelevant.
type cacheControlOption struct{}

func (cacheControlOption) IsInvokeOption() {}

// WithCacheControl returns an InvokeOption that, when supplied, causes the
// provider to emit Anthropic-style cache_control:{type:ephemeral} blocks on
// (a) the system message content, (b) the last tool definition, and
// (c) the last user/assistant text content part of the outgoing request.
// On hosts that ignore unknown fields (e.g. raw OpenAI) the option is a
// no-op; on OpenRouter and Anthropic-via-OpenRouter it is honored and
// produces the full Anthropic prompt-cache discount.
func WithCacheControl() provider.InvokeOption {
	return cacheControlOption{}
}

// config holds the build-time configuration for the Provider.
type config struct {
	apiKey           string
	baseURL          string
	httpClient       option.HTTPClient
	tracer           trace.Tracer
	reasoningInclude *bool
}

// Option configures a Provider via the functional options pattern.
type Option func(*config)

// WithAPIKey sets the API key for the OpenAI-compatible provider.
func WithAPIKey(key string) Option {
	return func(c *config) {
		c.apiKey = key
	}
}

// WithBaseURL sets a custom API base URL (e.g., for local proxies).
func WithBaseURL(url string) Option {
	return func(c *config) {
		c.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client for the provider. This is primarily
// useful for testing.
func WithHTTPClient(client option.HTTPClient) Option {
	return func(c *config) {
		c.httpClient = client
	}
}

// WithTracer configures an OpenTelemetry tracer for the provider.
func WithTracer(tracer trace.Tracer) Option {
	return func(c *config) {
		c.tracer = tracer
	}
}

// New creates an OpenAI-compatible provider. The model identity is
// supplied per-call via the [models.Spec] argument to [Provider.Invoke];
// there is no constructor option for it.
func New(opts ...Option) (*Provider, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.apiKey == "" {
		return nil, fmt.Errorf("missing required option: apiKey")
	}

	sdkOpts := []option.RequestOption{option.WithAPIKey(cfg.apiKey)}
	if cfg.baseURL != "" {
		sdkOpts = append(sdkOpts, option.WithBaseURL(cfg.baseURL))
	}
	if cfg.httpClient != nil {
		sdkOpts = append(sdkOpts, option.WithHTTPClient(cfg.httpClient))
	}

	return &Provider{
		client:           openai.NewClient(sdkOpts...),
		tracer:           cfg.tracer,
		includeReasoning: wantsReasoningInclude(cfg),
		isOpenRouter:     isOpenRouter(cfg.baseURL),
	}, nil
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// assistantReasoning is the per-turn reasoning replay payload collected
// by serializeMessages. Each entry corresponds to one assistant turn in
// state order, and carries the reasoning blocks that the read-side
// emitted in arrival order. The order is preserved by the loop that
// builds the slice; downstream consumers (the applyReasoningReplay
// mutation) must preserve it on the wire.
type assistantReasoning struct {
	Reasonings []artifact.Reasoning
	Sigs       []artifact.ReasoningSignature
}

// serializeMessages converts ore state into OpenAI chat completion message
// parameters. It maps ore roles to OpenAI message types and preserves
// ToolCall and ToolResult artifacts for tool calling conversations.
//
// The second return value is the per-turn reasoning payload collected
// from each assistant turn. It is consumed by applyReasoningReplay
// after the messages slice has been marshaled. Order matches state
// turn order, which matches read-side arrival order.
func (p *Provider) serializeMessages(s state.State) ([]openai.ChatCompletionMessageParamUnion, []assistantReasoning) {
	turns := s.Turns()
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(turns))
	replayPerTurn := make([]assistantReasoning, 0, len(turns))

	for _, turn := range turns {
		switch turn.Role {
		case state.RoleSystem:
			content := concatText(turn.Artifacts)
			messages = append(messages, openai.SystemMessage(content))
		case state.RoleUser:
			content := concatText(turn.Artifacts)
			messages = append(messages, openai.UserMessage(content))
		case state.RoleAssistant:
			var toolCalls []artifact.ToolCall
			var textContent string
			var reasonings []artifact.Reasoning
			var reasonSigs []artifact.ReasoningSignature
			for _, art := range turn.Artifacts {
				switch a := art.(type) {
				case artifact.Text:
					if textContent != "" {
						textContent += "\n"
					}
					textContent += a.Content
				case artifact.ToolCall:
					toolCalls = append(toolCalls, a)
				case artifact.Reasoning:
					reasonings = append(reasonings, a)
				case artifact.ReasoningSignature:
					// Drop cross-provider signatures on the openai
					// wire. Anthropic-only signatures cannot be
					// represented on the openai chat-completions
					// wire, and a future openai native signature
					// would be carried as `reasoning_details[]` on
					// the assistant message — but the openai
					// native SDK does not currently surface a
					// signature we can replay, so we drop all of
					// them for now. The upstream carry happens in
					// the dedicated sub-slices below.
					if a.Provider == "openai" && a.SubKind == "encrypted" {
						reasonSigs = append(reasonSigs, a)
					}
				}
			}

			// Save the reasoning artifacts for the post-serialize
			// mutation. Order is preserved per-turn (the order
			// artifacts appeared in the state's turn slice, which
			// is the order they were emitted by the previous
			// read).
			if len(reasonings) > 0 || len(reasonSigs) > 0 {
				replayPerTurn = append(replayPerTurn, assistantReasoning{
					Reasonings: reasonings,
					Sigs:       reasonSigs,
				})
			}

			if len(toolCalls) > 0 {
				tcParams := make([]openai.ChatCompletionMessageToolCallParam, len(toolCalls))
				for i, tc := range toolCalls {
					tcParams[i] = openai.ChatCompletionMessageToolCallParam{
						ID: tc.ID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					}
				}
				assistantMsg := openai.ChatCompletionAssistantMessageParam{
					ToolCalls: tcParams,
				}
				if textContent != "" {
					assistantMsg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: param.NewOpt(textContent),
					}
				}
				messages = append(messages, openai.ChatCompletionMessageParamUnion{
					OfAssistant: &assistantMsg,
				})
			} else {
				messages = append(messages, openai.AssistantMessage(textContent))
			}
		case state.RoleTool:
			var toolMsgs []openai.ChatCompletionMessageParamUnion
			for _, art := range turn.Artifacts {
				if tr, ok := art.(artifact.ToolResult); ok {
					toolMsgs = append(toolMsgs, openai.ToolMessage(tr.LLMString(), tr.ToolCallID))
				}
			}
			if len(toolMsgs) > 0 {
				messages = append(messages, toolMsgs...)
			} else {
				// Fallback: non-ToolResult artifacts in RoleTool turns are treated as
				// user messages for backward compatibility.
				content := concatText(turn.Artifacts)
				messages = append(messages, openai.UserMessage(content))
			}
		default:
			content := concatText(turn.Artifacts)
			messages = append(messages, openai.UserMessage(content))
		}
	}

	return messages, replayPerTurn
}

// concatText extracts and concatenates Text artifacts from a slice.
func concatText(artifacts []artifact.Artifact) string {
	var content string
	for _, art := range artifacts {
		if text, ok := art.(artifact.Text); ok {
			if content != "" {
				content += "\n"
			}
			content += text.Content
		}
	}
	return content
}

// Invoke serializes state into an OpenAI streaming chat completions request
// via the SDK and emits canonical artifact types in native SSE arrival order.
// Tool call fragments are assembled into complete ToolCall artifacts;
// text and reasoning deltas are emitted directly without accumulation.
//
// The spec carries the model identity and inference configuration. The
// adapter translates spec fields to the OpenAI wire format: spec.Name
// is the model identifier, spec.Temperature is the sampling
// temperature, spec.ThinkingLevel is mapped to reasoning_effort,
// spec.MaxOutputTokens is mapped to max_tokens, and spec.StopSequences
// to the stop field.
func (p *Provider) Invoke(ctx context.Context, s state.State, spec models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	var span trace.Span
	if p.tracer != nil {
		ctx, span = p.tracer.Start(ctx, "provider.invoke", trace.WithSpanKind(trace.SpanKindClient))
		// Note: the model attribute is set after the spec-walk so it
		// reflects the *effective* (per-call) model. A trace that lies
		// about which model served a request makes per-invocation
		// switching impossible to debug.
		defer span.End()
		// Attach httptrace hooks to record granular HTTP lifecycle events
		// (DNS, connect, TLS, first-byte) on the provider.invoke span.
		// This is only enabled when a tracer is configured via WithTracer.
		ctx = httptrace.WithClientTrace(ctx, otelhttptrace.NewClientTrace(ctx, otelhttptrace.WithoutSubSpans()))
	}

	messages, replayPerTurn := p.serializeMessages(s)

	var tools []tool.Tool
	var temperature float64
	var thinkingLevel models.ThinkingLevel
	var maxTokens int64
	var sessionID string
	var cacheControl bool
	var stopSequences []string
	for _, opt := range opts {
		if to, ok := opt.(provider.ToolsOption); ok {
			tools = to.Tools(ctx, s)
		}
		if temp, ok := opt.(temperatureOption); ok {
			temperature = temp.t
		}
		if tl, ok := opt.(thinkingLevelOption); ok {
			thinkingLevel = tl.level
		}
		if mto, ok := opt.(maxTokensOption); ok {
			maxTokens = mto.n
		}
		if mto, ok := opt.(provider.MaxTokensOption); ok {
			// Provider-agnostic form. N <= 0 is "no opinion";
			// the adapter does not set the wire field. Callers
			// (e.g. SummarizeStrategy) use this option to size
			// their own budget without importing a concrete
			// adapter package.
			if mto.N > 0 {
				maxTokens = mto.N
			}
		}
		if sid, ok := opt.(sessionIDOption); ok {
			sessionID = sid.id
		}
		if _, ok := opt.(cacheControlOption); ok {
			cacheControl = true
		}
	}

	// Spec fields take precedence over the per-call options above. A
	// spec field that is the zero value (empty Name, nil pointer, etc.)
	// leaves the corresponding value untouched. The spec is the
	// canonical source of truth for model identity and inference
	// configuration.
	if spec.Name != "" {
		// spec.Name is the model identifier; the per-call options
		// don't override this — there is no per-call model-name
		// option anymore.
	}
	if spec.Temperature != nil {
		temperature = *spec.Temperature
	}
	if spec.ThinkingLevel != "" {
		thinkingLevel = spec.ThinkingLevel
	}
	if spec.MaxOutputTokens > 0 {
		maxTokens = spec.MaxOutputTokens
	}
	if len(spec.StopSequences) > 0 {
		stopSequences = spec.StopSequences
	}

	// The spec's Name is the only source of model identity. An empty
	// Name is a hard error: we cannot issue a request without knowing
	// which model to call.
	if spec.Name == "" {
		return fmt.Errorf("openai: spec.Name is empty; model identity is required")
	}
	effectiveModel := spec.Name

	if p.tracer != nil {
		span.SetAttributes(attribute.String("model", effectiveModel))
		if id, ok := loop.ThreadIDFrom(ctx); ok {
			span.SetAttributes(attribute.String("thread_id", id))
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:         openai.ChatModel(effectiveModel),
		Messages:      messages,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: param.NewOpt(true)},
	}
	if len(stopSequences) > 0 {
		params.Stop = stopSequences
	}
	if len(tools) > 0 {
		params.Tools = p.serializeTools(tools)
	}
	if temperature != 0 {
		params.Temperature = param.NewOpt(temperature)
	}
	if effort := translateThinkingLevel(thinkingLevel); effort != "" {
		params.ReasoningEffort = openai.ReasoningEffort(effort)
	}
	if maxTokens > 0 {
		params.MaxTokens = param.NewOpt(maxTokens)
	}
	if sessionID != "" {
		// OpenAI native uses this for prefix-cache affinity; on other hosts
		// it is informational or ignored.
		params.PromptCacheKey = param.NewOpt(sessionID)
	}

	// Build the per-host extra fields. Multiple concerns share the same
	// SetExtraFields bucket because the SDK's metadata implementation
	// replaces the whole extra map on each call, so we collect them
	// here and dispatch once.
	//
	// Mutation order:
	//   1. applyCacheControl (if WithCacheControl) — typed messages
	//      round-trip through JSON, stamps the three Anthropic-style
	//      cache_control blocks.
	//   2. applyReasoningReplayToAny — attaches the host-specific
	//      reasoning field (reasoning_content for OpenAI native,
	//      reasoning_details[] for OpenRouter) to each assistant
	//      message that carried reasoning artifacts in state.
	//
	// The two mutations compose by mutating in place: when both are
	// needed, applyCacheControl produces the []any, and
	// applyReasoningReplayToAny mutates that same slice. The final
	// result is shipped via a single SetExtraFields call.
	extra := map[string]any{}
	if cacheControl {
		mutMsgs, mutTools, err := applyCacheControl(messages, params.Tools)
		if err != nil {
			return fmt.Errorf("apply cache control: %w", err)
		}
		mutMsgs, err = applyReasoningReplayToAny(mutMsgs, replayPerTurn, p.isOpenRouter)
		if err != nil {
			return fmt.Errorf("apply reasoning replay: %w", err)
		}
		extra["messages"] = mutMsgs
		if mutTools != nil {
			extra["tools"] = mutTools
		}
	} else if len(replayPerTurn) > 0 {
		// No cache control, but reasoning replay is needed. Round-trip
		// the typed messages through JSON once to obtain the mutable
		// []any shape.
		msgBytes, err := json.Marshal(messages)
		if err != nil {
			return fmt.Errorf("marshal messages: %w", err)
		}
		var mutMsgs []any
		if err := json.Unmarshal(msgBytes, &mutMsgs); err != nil {
			return fmt.Errorf("unmarshal messages: %w", err)
		}
		mutMsgs, err = applyReasoningReplayToAny(mutMsgs, replayPerTurn, p.isOpenRouter)
		if err != nil {
			return fmt.Errorf("apply reasoning replay: %w", err)
		}
		extra["messages"] = mutMsgs
	}
	if p.includeReasoning {
		// Inject `reasoning: {include: true}` when talking to OpenRouter
		// (or when the caller has explicitly opted in via
		// WithReasoningInclude). Without this field, OpenRouter silently
		// drops `delta.reasoning_content` from the SSE stream, which
		// would defeat the read-side handling at the bottom of this
		// function. The field is only emitted on resolve at construction,
		// so other OpenAI-compatible providers never see it.
		extra["reasoning"] = map[string]any{"include": true}
	}
	if len(extra) > 0 {
		params.SetExtraFields(extra)
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	// pendingStopReason buffers the most-recent non-empty finish_reason
	// from the stream so it can be emitted as a single StopReason
	// artifact after the loop ends. The OpenAI wire format sends
	// `finish_reason: null` on every intermediate delta and only sets a
	// real value on the final delta of a choice; the zero-value check
	// in translateFinishReason prevents the intermediate nulls from
	// clobbering a previously-buffered reason.
	var pendingStopReason artifact.StopReasonKind

	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) == 0 {
			if chunk.Usage.TotalTokens > 0 {
				cacheRead, cacheWrite := readCacheUsage(chunk.Usage)
				select {
				case ch <- artifact.Usage{
					PromptTokens:     int(chunk.Usage.PromptTokens),
					CompletionTokens: int(chunk.Usage.CompletionTokens),
					TotalTokens:      int(chunk.Usage.TotalTokens),
					CacheReadTokens:  cacheRead,
					CacheWriteTokens: cacheWrite,
				}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			continue
		}

		// Buffer the finish_reason. OpenAI sends `null` on every
		// intermediate delta and only sets a real value on the final
		// delta; translateFinishReason returns "" for the empty /
		// null case so the buffer is not clobbered.
		if reason := translateFinishReason(chunk.Choices[0].FinishReason); reason != "" {
			pendingStopReason = reason
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			select {
			case ch <- artifact.TextDelta{Content: delta.Content}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if field, ok := delta.JSON.ExtraFields["reasoning_content"]; ok {
			var reasoning string
			if err := json.Unmarshal([]byte(field.Raw()), &reasoning); err == nil && reasoning != "" {
				select {
				case ch <- artifact.ReasoningDelta{Content: reasoning}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		// `delta.reasoning` is the flat-string form surfaced by some
		// OpenAI-compatible providers (e.g. OpenRouter for non-Anthropic
		// reasoning routes). The `reasoning_content` path above
		// remains the canonical OpenAI-native shape; both paths
		// coexist so a single provider can serve multiple hosts.
		if field, ok := delta.JSON.ExtraFields["reasoning"]; ok {
			var reasoning string
			if err := json.Unmarshal([]byte(field.Raw()), &reasoning); err == nil && reasoning != "" {
				select {
				case ch <- artifact.ReasoningDelta{Content: reasoning}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		// `delta.reasoning_details[]` is the structured-form used by
		// OpenRouter and other proxies. Each entry has a `type`
		// discriminator that drives emission: `reasoning.text` becomes
		// a ReasoningDelta, `reasoning.encrypted` becomes a
		// ReasoningSignature (so it can be replayed on the next turn).
		if field, ok := delta.JSON.ExtraFields["reasoning_details"]; ok {
			arts, err := reasoningDetailsToArtifacts([]byte(field.Raw()))
			if err == nil {
				for _, a := range arts {
					select {
					case ch <- a:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
		}

		for _, tc := range delta.ToolCalls {
			select {
			case ch <- artifact.ToolCallDelta{
				Index:     int(tc.Index),
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	if err := stream.Err(); err != nil {
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return fmt.Errorf("streaming chat completion: %w", err)
	}

	// Emit the buffered StopReason, if any. This is unconditional
	// across the success path: a successful stream that did not
	// report a finish_reason (rare, but possible on some
	// OpenAI-compatible hosts) leaves the buffer empty and we
	// skip the send. The StopReason is emitted before the final
	// Usage; the only artifact after it on the channel would be
	// the Usage, which has already been sent inline by the loop
	// above when chunk.Choices is empty.
	if pendingStopReason != "" {
		select {
		case ch <- artifact.StopReason{Reason: pendingStopReason}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// translateFinishReason normalizes an OpenAI finish_reason value into
// the canonical artifact.StopReasonKind used across all adapters.
// The OpenAI SDK exposes FinishReason as a plain string (not a typed
// enum), so the comparison is against the documented wire values.
// The mapping is:
//
//   - "stop" → stop
//   - "length" → length
//   - "tool_calls" and the deprecated "function_call" → tool_use
//   - "content_filter" → refusal
//   - anything else (or the empty string returned for
//     `finish_reason: null` intermediates) → other or ""
//
// The empty input maps to the empty kind so the caller's
// "non-zero reason buffered" check can distinguish "upstream
// reported a known reason" from "upstream did not report one."
func translateFinishReason(r string) artifact.StopReasonKind {
	switch r {
	case "":
		return ""
	case "stop":
		return artifact.StopReasonStop
	case "length":
		return artifact.StopReasonLength
	case "tool_calls", "function_call":
		// function_call is the pre-tool-calls wire form; it is
		// structurally equivalent to tool_use for canonical
		// purposes (a model asked to invoke a callable).
		return artifact.StopReasonToolUse
	case "content_filter":
		return artifact.StopReasonRefusal
	default:
		return artifact.StopReasonOther
	}
}

// serializeTools converts provider-agnostic tool definitions into OpenAI SDK
// tool parameters.
func (p *Provider) serializeTools(tools []tool.Tool) []openai.ChatCompletionToolParam {
	toolParams := make([]openai.ChatCompletionToolParam, len(tools))
	for i, t := range tools {
		fnDef := openai.FunctionDefinitionParam{
			Name:       t.Name,
			Parameters: openai.FunctionParameters(t.Schema),
		}
		if t.Description != "" {
			fnDef.Description = param.NewOpt(t.Description)
		}
		toolParams[i] = openai.ChatCompletionToolParam{
			Function: fnDef,
		}
	}
	return toolParams
}

// cacheControlEphemeral is the Anthropic-style cache-control block value. It
// is the only value the hosts (OpenRouter, Anthropic-via-OpenRouter) honor
// for prompt caching today. Stored as a package-level constant so the
// mutation helpers below reference a single source of truth.
var cacheControlEphemeral = map[string]any{"type": "ephemeral"}

// readCacheUsage extracts the cache read and write token counts from a
// streaming usage chunk. The function prefers Anthropic-style fields
// (cache_read_input_tokens / cache_creation_input_tokens, surfaced by
// OpenRouter and Anthropic-via-OpenRouter) when they are present, and
// falls back to the OpenAI-native field
// (usage.prompt_tokens_details.cached_tokens) otherwise. Hosts that report
// neither leave both return values at zero, which the artifact's
// `omitempty` JSON tag will hide from the wire.
//
// The function is tolerant of malformed or non-numeric values: a parse
// failure on either Anthropic-style field is treated as zero rather than
// an error, because a single bad number in a usage chunk should not
// abort an in-flight stream.
//
// Note on presence detection: the openai-go v1.12 apijson decoder marks
// unmodeled extra fields with status `invalid` whenever the typed
// respjson.Field cannot consume the JSON value's runtime type (an
// integer for cache_read_input_tokens, in our case). The raw JSON is
// still preserved on the field; we therefore use `Raw() != ""` as the
// presence test rather than `Valid()`.
func readCacheUsage(usage openai.CompletionUsage) (cacheRead, cacheWrite int) {
	// Anthropic-via-OpenRouter and raw Anthropic on OpenRouter surface
	// cache metrics at the top of the usage object. The SDK does not
	// model these, so they arrive in the JSON extra-fields bucket.
	if field, ok := usage.JSON.ExtraFields["cache_read_input_tokens"]; ok && field.Raw() != "" && field.Raw() != "null" {
		var n int64
		if err := json.Unmarshal([]byte(field.Raw()), &n); err == nil {
			cacheRead = int(n)
		}
		if field, ok := usage.JSON.ExtraFields["cache_creation_input_tokens"]; ok && field.Raw() != "" && field.Raw() != "null" {
			var n int64
			if err := json.Unmarshal([]byte(field.Raw()), &n); err == nil {
				cacheWrite = int(n)
			}
		}
		return cacheRead, cacheWrite
	}
	// OpenAI native reports cache hits inside prompt_tokens_details.
	// Prefer the explicit int64 field on the typed struct over the
	// JSON fallback.
	cacheRead = int(usage.PromptTokensDetails.CachedTokens)
	return cacheRead, 0
}

// reasoningDetailsToArtifacts walks a `delta.reasoning_details[]` JSON
// payload and emits the corresponding streaming artifacts. Supported
// entry shapes today:
//
//   - {"type": "reasoning.text", "text": "..."}        -> ReasoningDelta
//   - {"type": "reasoning.encrypted", "data": "..."}  -> ReasoningSignature
//
// Unknown `type` values are skipped silently (the wire is allowed to
// grow new kinds without breaking the SDK). Malformed JSON is reported
// to the caller; the streaming loop in Invoke treats a parse failure
// as a no-op for the affected delta so a single bad chunk does not
// abort the in-flight stream.
func reasoningDetailsToArtifacts(rawJSON []byte) ([]artifact.Artifact, error) {
	var entries []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(rawJSON, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal reasoning_details: %w", err)
	}

	var out []artifact.Artifact
	for _, e := range entries {
		switch e.Type {
		case "reasoning.text":
			if e.Text == "" {
				continue
			}
			out = append(out, artifact.ReasoningDelta{Content: e.Text})
		case "reasoning.encrypted":
			if e.Data == "" {
				continue
			}
			out = append(out, artifact.ReasoningSignature{
				Provider: "openai",
				SubKind:  "encrypted",
				Data:     e.Data,
			})
		}
	}
	return out, nil
}

// applyReasoningReplayToAny mutates an already-marshaled []any messages
// slice to attach the host-specific reasoning replay field on each
// assistant message. It is the second-stage helper in the chain:
// applyCacheControl produces a []any via the SDK's typed-to-any
// round-trip, and this function then mutates the assistant entries.
//
// The `isOpenRouter` boolean is the same one resolved at Provider
// construction time (see New); threading it explicitly here keeps the
// helper a pure function and unit-testable, while still guaranteeing
// the read- and write-sides agree on the host identity.
func applyReasoningReplayToAny(messages []any, perTurn []assistantReasoning, isOpenRouter bool) ([]any, error) {
	if len(perTurn) == 0 {
		return messages, nil
	}

	// Walk the marshaled messages in lockstep with the per-turn
	// replay payload, matching each assistant message to its
	// payload by ordinal.
	assistantIdx := 0
	for _, m := range messages {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		if role != "assistant" {
			continue
		}

		// Bounds check: the typed slice can contain zero assistant
		// messages in degenerate states. Skip cleanly rather than
		// panic.
		if assistantIdx >= len(perTurn) {
			break
		}
		entry := perTurn[assistantIdx]
		assistantIdx++

		if len(entry.Reasonings) == 0 && len(entry.Sigs) == 0 {
			continue
		}

		if isOpenRouter {
			details := buildReasoningDetails(entry)
			if len(details) > 0 {
				mm["reasoning_details"] = details
			}
			continue
		}

		// OpenAI native: concatenate reasoning content into the
		// assistant message's `reasoning_content` field. Signature
		// entries (Provider=="openai", SubKind=="encrypted") are
		// dropped — the native wire has no surface for them.
		var sb strings.Builder
		for _, r := range entry.Reasonings {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(r.Content)
		}
		if sb.Len() > 0 {
			mm["reasoning_content"] = sb.String()
		}
	}

	return messages, nil
}

// buildReasoningDetails turns a per-turn reasoning payload into the
// reasoning_details[] wire array. Order matches the order artifacts
// appeared in the state's turn slice, which matches read-side arrival
// order. The stable `id` field is derived from the artifact's
// position so multi-turn replay produces stable ids across
// requests.
func buildReasoningDetails(entry assistantReasoning) []map[string]any {
	var out []map[string]any
	idx := 0
	for _, r := range entry.Reasonings {
		if r.Content == "" {
			continue
		}
		out = append(out, map[string]any{
			"type": "reasoning.text",
			"id":   "rd-" + strconv.Itoa(idx),
			"text": r.Content,
		})
		idx++
	}
	for _, s := range entry.Sigs {
		if s.Provider != "openai" || s.SubKind != "encrypted" || s.Data == "" {
			continue
		}
		out = append(out, map[string]any{
			"type": "reasoning.encrypted",
			"id":   "rd-" + strconv.Itoa(idx),
			"data": s.Data,
		})
		idx++
	}
	return out
}

// applyCacheControl mutates the marshaled messages and tools slices to add
// Anthropic-style cache_control:{type:ephemeral} blocks at the three
// locations the pi reference implementation targets: the system message,
// the last tool definition, and the last user/assistant text content part.
// It returns the mutated slices in a form that can be passed to
// params.SetExtraFields to override the SDK's typed Messages and Tools
// fields (the openai-go v1.12 SDK does not model cache_control, so the
// typed path cannot carry the blocks).
//
// If the input marshaling fails, the function returns the error and the
// caller should propagate it. The mutation is conservative: it only
// touches the targeted locations and leaves every other field unchanged.
// If a target is absent (no system message, no tools, no text-bearing
// user/assistant turn) that stamp is silently skipped.
func applyCacheControl(messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolParam) (mutatedMessages []any, mutatedTools []any, err error) {
	// Marshal the typed messages slice to JSON, then unmarshal to a
	// mutable []any / map[string]any shape. The round-trip is the only
	// way to take ownership of the SDK's union types for mutation.
	msgBytes, err := json.Marshal(messages)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal messages: %w", err)
	}
	var msgAny []any
	if err := json.Unmarshal(msgBytes, &msgAny); err != nil {
		return nil, nil, fmt.Errorf("unmarshal messages: %w", err)
	}

	// Stamp the system message. There is conventionally at most one; we
	// stamp the first match and break.
	for _, m := range msgAny {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if mm["role"] == "system" {
			if content, ok := mm["content"].(string); ok {
				mm["content"] = []any{
					map[string]any{
						"type":          "text",
						"text":          content,
						"cache_control": cacheControlEphemeral,
					},
				}
			}
			break
		}
	}

	// Walk back from the end to find the last user/assistant message
	// that carries non-empty text content. Tool messages and assistant
	// tool-call-only messages are skipped; assistant messages that
	// contain both text and tool calls still count, because their
	// `content` field is the text. The first match wins; the loop
	// terminates immediately after stamping.
	for i := len(msgAny) - 1; i >= 0; i-- {
		m, ok := msgAny[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role != "user" && role != "assistant" {
			continue
		}
		// Assistant messages that are tool-call-only (no text content)
		// leave `content` absent or null in the marshaled JSON; the
		// type assertion below handles both cases.
		content, _ := m["content"].(string)
		if content == "" {
			continue
		}
		m["content"] = []any{
			map[string]any{
				"type":          "text",
				"text":          content,
				"cache_control": cacheControlEphemeral,
			},
		}
		break
	}

	mutatedMessages = msgAny

	// Stamp the last tool definition's `function` block. If no tools
	// are present, leave the tools override unset so SetExtraFields
	// does not include an empty `tools` key in the request body.
	if len(tools) > 0 {
		toolBytes, err := json.Marshal(tools)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal tools: %w", err)
		}
		var toolAny []any
		if err := json.Unmarshal(toolBytes, &toolAny); err != nil {
			return nil, nil, fmt.Errorf("unmarshal tools: %w", err)
		}
		if n := len(toolAny); n > 0 {
			if last, ok := toolAny[n-1].(map[string]any); ok {
				if fn, ok := last["function"].(map[string]any); ok {
					fn["cache_control"] = cacheControlEphemeral
				}
			}
		}
		mutatedTools = toolAny
	}

	return mutatedMessages, mutatedTools, nil
}
