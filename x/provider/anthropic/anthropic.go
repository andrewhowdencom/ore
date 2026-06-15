// Package anthropic implements a provider adapter for the Anthropic Messages
// API and the OpenRouter /api/v1/messages mirror. It wraps the official
// github.com/anthropics/anthropic-sdk-go client.
package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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
	model  string
	tracer trace.Tracer
	// isOpenRouter is the resolved boolean used to choose the
	// OpenRouter branch (`Authorization: Bearer <key>`) over the
	// Anthropic native branch (`x-api-key: <key>`). Resolved at
	// construction from the configured base URL so the auth header
	// cannot drift between turns of the same Provider.
	isOpenRouter bool
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
// tokens the model is permitted to generate on a single invocation. Callers
// are responsible for picking a value appropriate to the model; different
// Anthropic models have different ceilings. The default (when this option is
// not supplied) is 1, the SDK's hard minimum, which will let the provider
// fail loudly rather than silently truncating on an arbitrary cap.
func WithMaxTokens(n int64) provider.InvokeOption {
	return maxTokensOption{n: n}
}

// thinkingBudgetOption is a per-invocation option that sets the
// extended-thinking budget, in tokens, for models that support it
// (e.g. claude-3-7-sonnet). A budget of 0 is a no-op: the SDK will not
// receive a `thinking` field, so the upstream model will fall back to its
// non-thinking default behavior.
type thinkingBudgetOption struct {
	tokens int64
}

func (thinkingBudgetOption) IsInvokeOption() {}

// WithThinkingBudget returns an InvokeOption that sets the extended-thinking
// budget, in tokens, for a single provider invocation. A value of 0 is a
// no-op (the request is sent without a `thinking` field). Anthropic requires
// a minimum budget of 1,024 tokens; smaller values are forwarded as-is and
// the upstream rejects them.
func WithThinkingBudget(tokens int64) provider.InvokeOption {
	return thinkingBudgetOption{tokens: tokens}
}

// invokeOptions is the resolved per-invocation configuration collected by
// Invoke. Each field has its own default so a missing option does not
// silently change behavior.
type invokeOptions struct {
	temperature     float64
	temperatureSet  bool
	maxTokens       int64
	maxTokensSet    bool
	thinkingBudget  int64
	thinkingBudgetSet bool
	tools           []tool.Tool
	toolsSet        bool
}

// applyInvokeOptions walks the provider.InvokeOption list and folds it into
// an invokeOptions struct. Unknown option types are ignored; only the
// temperature / maxTokens / thinkingBudget / tools options are recognized
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
		case thinkingBudgetOption:
			out.thinkingBudget = o.tokens
			out.thinkingBudgetSet = true
		case provider.ToolsOption:
			// The carrier's Tools field is a function so the
			// tool list can be resolved dynamically from
			// (ctx, state). At this stage we resolve it
			// eagerly with a no-op context/state because the
			// list is static for the duration of the call;
			// the framework supplies the live values at
			// the call site, not here.
			out.tools = o.Tools(context.Background(), state.NewBuffer())
			out.toolsSet = true
		}
	}
	return out
}

// config holds the build-time configuration for the Provider.
type config struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient option.HTTPClient
	tracer     trace.Tracer
}

// Option configures a Provider via the functional options pattern.
type Option func(*config)

// WithAPIKey sets the API key for the Anthropic-compatible provider. The key
// is interpreted as an Anthropic native key when the configured base URL is
// empty or targets api.anthropic.com; it is interpreted as an OpenRouter key
// when the configured base URL targets openrouter.ai.
func WithAPIKey(key string) Option {
	return func(c *config) {
		c.apiKey = key
	}
}

// WithModel sets the model identifier for the Anthropic-compatible provider.
func WithModel(model string) Option {
	return func(c *config) {
		c.model = model
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

// New creates an Anthropic-compatible provider. WithAPIKey and WithModel are
// required; all other options are optional. The base URL is inspected once at
// construction time to decide which auth header to apply, so callers do not
// need to track host identity across invocations.
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

	sdkOpts := buildSDKOptions(cfg)

	return &Provider{
		client:      anthropic.NewClient(sdkOpts...),
		model:       cfg.model,
		tracer:      cfg.tracer,
		isOpenRouter: isOpenRouter(cfg.baseURL),
	}, nil
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// Invoke is the streaming entry point. The skeleton in this task returns
// nil unconditionally; full implementation lands in later tasks. The stub
// signature matches provider.Provider so the compile-time interface check
// succeeds and downstream callers can wire the provider up before
// streaming is implemented.
func (p *Provider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	_ = ctx
	_ = s
	_ = ch
	_ = opts
	return nil
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
func (p *Provider) serializeMessages(s state.State) serializeResult {
	turns := s.Turns()
	out := serializeResult{
		messages: make([]anthropic.MessageParam, 0, len(turns)),
	}

	for _, turn := range turns {
		switch turn.Role {
		case state.RoleSystem:
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
		case state.RoleUser:
			txt := concatText(turn.Artifacts)
			out.messages = append(out.messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(txt),
			))
		case state.RoleAssistant:
			out.messages = append(out.messages, serializeAssistantTurn(turn.Artifacts))
		case state.RoleTool:
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
// The returned MessageParam always has the assistant role. An empty
// artifact slice still produces a valid (if empty) assistant message
// because callers always need to emit a message for the turn.
func serializeAssistantTurn(artifacts []artifact.Artifact) anthropic.MessageParam {
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

	return anthropic.NewAssistantMessage(blocks...)
}

// parseToolArguments converts an artifact.ToolCall's argument payload
// into the form the SDK's tool_use block expects (`any`). The two
// available inputs are:
//
//   - Value: a structured Go value. When set, it is forwarded as-is.
//   - Arguments: a JSON-encoded string. When Value is nil, we try to
//     unmarshal Arguments; on failure, the raw string is passed
//     through so the upstream can produce a useful error.
//
// Both cases end up as `any` so the SDK's tool_use block can serialize
// the input to JSON without further conversion.
func parseToolArguments(tc artifact.ToolCall) any {
	if tc.Value != nil {
		return tc.Value
	}
	if tc.Arguments == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(tc.Arguments), &v); err == nil {
		return v
	}
	return tc.Arguments
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
// non-text content silently). Mixed content in a system turn is
// unexpected; the framework does not currently emit such turns, so the
// conservative policy is to drop them.
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
