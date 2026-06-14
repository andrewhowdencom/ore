// Package openai implements a provider adapter for OpenAI-compatible chat
// completions APIs. It wraps the official github.com/openai/openai-go client.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptrace"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
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
	model  string
	tracer trace.Tracer
	// includeReasoning is the resolved decision about whether to opt into
	// upstream reasoning traces via `reasoning: {include: true}` in the
	// request body. Resolved once at construction from the explicit
	// override and the base-URL heuristic so Invoke doesn't re-walk options.
	includeReasoning bool
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

// reasoningEffortOption is a per-invocation option that sets the reasoning
// effort for models that support it (e.g. o3-mini).
type reasoningEffortOption struct {
	effort string
}

func (reasoningEffortOption) IsInvokeOption() {}

// WithReasoningEffort returns an InvokeOption that sets the reasoning effort
// for a single provider invocation. Supported values are "low", "medium", and
// "high".
func WithReasoningEffort(effort string) provider.InvokeOption {
	return reasoningEffortOption{effort: effort}
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
	model            string
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

// WithModel sets the model identifier for the OpenAI-compatible provider.
func WithModel(model string) Option {
	return func(c *config) {
		c.model = model
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

// New creates an OpenAI-compatible provider.
func New(opts ...Option) (*Provider, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.apiKey == "" {
		return nil, fmt.Errorf("missing required option: apiKey")
	}
	if cfg.model == "" {
		return nil, fmt.Errorf("missing required option: model")
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
		model:            cfg.model,
		tracer:           cfg.tracer,
		includeReasoning: wantsReasoningInclude(cfg),
	}, nil
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// serializeMessages converts ore state into OpenAI chat completion message
// parameters. It maps ore roles to OpenAI message types and preserves
// ToolCall and ToolResult artifacts for tool calling conversations.
func (p *Provider) serializeMessages(s state.State) []openai.ChatCompletionMessageParamUnion {
	turns := s.Turns()
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(turns))

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
			for _, art := range turn.Artifacts {
				switch a := art.(type) {
				case artifact.Text:
					if textContent != "" {
						textContent += "\n"
					}
					textContent += a.Content
				case artifact.ToolCall:
					toolCalls = append(toolCalls, a)
				}
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

	return messages
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
func (p *Provider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	var span trace.Span
	if p.tracer != nil {
		ctx, span = p.tracer.Start(ctx, "provider.invoke", trace.WithSpanKind(trace.SpanKindClient))
		// Note: the model attribute is set after the option-walk so it
		// reflects the *effective* (possibly overridden) model, not the
		// constructor default. A trace that lies about which model served
		// a request makes per-invocation switching impossible to debug.
		defer span.End()
		// Attach httptrace hooks to record granular HTTP lifecycle events
		// (DNS, connect, TLS, first-byte) on the provider.invoke span.
		// This is only enabled when a tracer is configured via WithTracer.
		ctx = httptrace.WithClientTrace(ctx, otelhttptrace.NewClientTrace(ctx, otelhttptrace.WithoutSubSpans()))
	}

	messages := p.serializeMessages(s)

	var tools []tool.Tool
	var temperature float64
	var reasoningEffort string
	var maxTokens int64
	var sessionID string
	var cacheControl bool
	var modelName string
	for _, opt := range opts {
		if to, ok := opt.(provider.ToolsOption); ok {
			tools = to.Tools(ctx, s)
		}
		if temp, ok := opt.(temperatureOption); ok {
			temperature = temp.t
		}
		if re, ok := opt.(reasoningEffortOption); ok {
			reasoningEffort = re.effort
		}
		if mto, ok := opt.(maxTokensOption); ok {
			maxTokens = mto.n
		}
		if sid, ok := opt.(sessionIDOption); ok {
			sessionID = sid.id
		}
		if _, ok := opt.(cacheControlOption); ok {
			cacheControl = true
		}
		if mo, ok := opt.(provider.ModelOption); ok {
			modelName = mo.Model
		}
	}

	// Effective model: per-invocation override wins; empty string falls
	// through to the constructor default. This is the only place the
	// precedence rule is encoded, and it is intentionally simple.
	effectiveModel := p.model
	if modelName != "" {
		effectiveModel = modelName
	}

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
	if len(tools) > 0 {
		params.Tools = p.serializeTools(tools)
	}
	if temperature != 0 {
		params.Temperature = param.NewOpt(temperature)
	}
	if reasoningEffort != "" {
		params.ReasoningEffort = openai.ReasoningEffort(reasoningEffort)
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
	// replaces the whole extra map on each call, so we collect them here
	// and dispatch once.
	extra := map[string]any{}
	if cacheControl {
		mutMsgs, mutTools, err := applyCacheControl(messages, params.Tools)
		if err != nil {
			return fmt.Errorf("apply cache control: %w", err)
		}
		extra["messages"] = mutMsgs
		if mutTools != nil {
			extra["tools"] = mutTools
		}
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

	return nil
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
						"type":         "text",
						"text":         content,
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
				"type":         "text",
				"text":         content,
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
