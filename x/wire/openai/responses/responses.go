package responses

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/tool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const defaultEndpoint = "https://api.openai.com/v1/responses"

// RequestEditor adds service-specific headers to an outgoing request. It is
// called immediately before each HTTP attempt. Editors must not retain req.
type RequestEditor func(context.Context, *http.Request) error

// Option configures a Responses wire provider.
type Option func(*config)

type config struct {
	endpoint     string
	httpClient   *http.Client
	requestEdit  RequestEditor
	nameResolver func(string) string
	tracer       trace.Tracer
}

// WithEndpoint selects the complete Responses endpoint URL.
func WithEndpoint(endpoint string) Option { return func(c *config) { c.endpoint = endpoint } }

// WithHTTPClient replaces the HTTP client. It is primarily useful in tests.
func WithHTTPClient(client *http.Client) Option { return func(c *config) { c.httpClient = client } }

// WithRequestEditor installs service-specific authentication and headers.
func WithRequestEditor(editor RequestEditor) Option {
	return func(c *config) { c.requestEdit = editor }
}

// WithNameResolver maps a canonical model name to the name accepted by the host.
func WithNameResolver(resolve func(string) string) Option {
	return func(c *config) { c.nameResolver = resolve }
}

// WithTracer enables provider invocation and HTTP lifecycle tracing.
func WithTracer(tracer trace.Tracer) Option { return func(c *config) { c.tracer = tracer } }

// Provider implements provider.Provider using the OpenAI Responses protocol.
type Provider struct {
	endpoint     string
	httpClient   *http.Client
	requestEdit  RequestEditor
	nameResolver func(string) string
	tracer       trace.Tracer
}

// New constructs a Responses wire provider.
func New(opts ...Option) (*Provider, error) {
	cfg := config{endpoint: defaultEndpoint, httpClient: http.DefaultClient}
	for _, opt := range opts {
		opt(&cfg)
	}
	if strings.TrimSpace(cfg.endpoint) == "" {
		return nil, errors.New("responses: endpoint is empty")
	}
	if cfg.httpClient == nil {
		return nil, errors.New("responses: HTTP client is nil")
	}
	if cfg.nameResolver == nil {
		cfg.nameResolver = func(name string) string { return name }
	}
	return &Provider{
		endpoint: cfg.endpoint, httpClient: cfg.httpClient,
		requestEdit: cfg.requestEdit, nameResolver: cfg.nameResolver, tracer: cfg.tracer,
	}, nil
}

// WithTools configures tools for one invocation.
func WithTools(tools []tool.Tool) provider.InvokeOption { return provider.WithTools(tools) }

type sessionIDOption struct{ id string }

func (sessionIDOption) IsInvokeOption() {}

// WithSessionID sets prompt_cache_key for one invocation.
func WithSessionID(id string) provider.InvokeOption { return sessionIDOption{id: id} }

type responseRequest struct {
	Model             string         `json:"model"`
	Instructions      string         `json:"instructions,omitempty"`
	Input             []any          `json:"input"`
	Tools             []responseTool `json:"tools,omitempty"`
	ToolChoice        string         `json:"tool_choice"`
	ParallelToolCalls bool           `json:"parallel_tool_calls"`
	Reasoning         *reasoning     `json:"reasoning,omitempty"`
	Temperature       *float64       `json:"temperature,omitempty"`
	TopP              *float64       `json:"top_p,omitempty"`
	MaxOutputTokens   int64          `json:"max_output_tokens,omitempty"`
	PromptCacheKey    string         `json:"prompt_cache_key,omitempty"`
	Store             bool           `json:"store"`
	Stream            bool           `json:"stream"`
	Include           []string       `json:"include"`
}

type reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type responseTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

// Invoke serializes the ledger, streams a Responses request, and emits ore artifacts.
func (p *Provider) Invoke(ctx context.Context, state ledger.State, spec models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	if spec.Name == "" {
		return errors.New("responses: spec.Name is empty; model identity is required")
	}
	ctx, span := p.startSpan(ctx, p.nameResolver(spec.Name))
	if span != nil {
		defer span.End()
	}

	reqBody := p.buildRequest(ctx, state, spec, opts)
	if span != nil && reqBody.PromptCacheKey != "" {
		span.SetAttributes(attribute.String("gen_ai.request.prompt_cache_key", reqBody.PromptCacheKey))
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("responses: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("responses: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.requestEdit != nil {
		if err := p.requestEdit(ctx, req); err != nil {
			return fmt.Errorf("responses: authorize request: %w", err)
		}
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.recordError(span, err)
		return fmt.Errorf("responses: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := newHTTPError(resp)
		p.recordError(span, err)
		return err
	}
	if err := consumeSSE(ctx, resp.Body, ch, span); err != nil {
		p.recordError(span, err)
		return fmt.Errorf("responses: read stream: %w", err)
	}
	return nil
}

func (p *Provider) buildRequest(ctx context.Context, state ledger.State, spec models.Spec, opts []provider.InvokeOption) responseRequest {
	instructions, input := serializeState(state)
	var tools []tool.Tool
	var sessionID string
	var maxTokens int64
	for _, opt := range opts {
		if v, ok := opt.(provider.ToolsOption); ok && v.Tools != nil {
			tools = v.Tools(ctx, state)
		}
		if v, ok := opt.(provider.MaxTokensOption); ok && v.N > 0 {
			maxTokens = v.N
		}
		if v, ok := opt.(sessionIDOption); ok {
			sessionID = v.id
		}
	}
	if spec.MaxOutputTokens > 0 {
		maxTokens = spec.MaxOutputTokens
	}
	result := responseRequest{
		Model: p.nameResolver(spec.Name), Instructions: instructions, Input: input,
		ToolChoice: "auto", ParallelToolCalls: true, Store: false, Stream: true,
		Include: []string{"reasoning.encrypted_content"}, Temperature: spec.Temperature,
		TopP: spec.TopP, MaxOutputTokens: maxTokens, PromptCacheKey: sessionID,
	}
	for _, item := range tools {
		result.Tools = append(result.Tools, responseTool{
			Type: "function", Name: item.Name, Description: item.Description,
			Parameters: item.Schema, Strict: false,
		})
	}
	if effort := thinkingEffort(spec.ThinkingLevel); effort != "" {
		result.Reasoning = &reasoning{Effort: effort, Summary: "auto"}
	}
	return result
}

func serializeState(state ledger.State) (string, []any) {
	if state == nil {
		return "", []any{}
	}
	var instructions []string
	var input []any
	for _, turn := range state.Turns() {
		if turn.Role == ledger.RoleSystem {
			if text := artifactText(turn.Artifacts); text != "" {
				instructions = append(instructions, text)
			}
			continue
		}
		switch turn.Role {
		case ledger.RoleTool:
			for _, art := range turn.Artifacts {
				if result, ok := art.(artifact.ToolResult); ok {
					input = append(input, map[string]any{"type": "function_call_output", "call_id": result.ToolCallID, "output": result.LLMString()})
				}
			}
		case ledger.RoleAssistant:
			input = appendAssistantItems(input, turn.Artifacts)
		default:
			content := []any{}
			if text := artifactText(turn.Artifacts); text != "" {
				content = append(content, map[string]any{"type": "input_text", "text": text})
			}
			for _, art := range turn.Artifacts {
				if image, ok := art.(artifact.Image); ok {
					content = append(content, map[string]any{"type": "input_image", "image_url": image.URL})
				}
			}
			if len(content) > 0 {
				input = append(input, map[string]any{"type": "message", "role": "user", "content": content})
			}
		}
	}
	return strings.Join(instructions, "\n\n"), input
}

func appendAssistantItems(input []any, artifacts []artifact.Artifact) []any {
	if text := artifactText(artifacts); text != "" {
		input = append(input, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}})
	}
	for _, art := range artifacts {
		switch value := art.(type) {
		case artifact.ToolCall:
			input = append(input, map[string]any{"type": "function_call", "call_id": value.ID, "name": value.Name, "arguments": value.Arguments})
		case artifact.ReasoningSignature:
			if value.Provider == "openai" && value.SubKind == "encrypted" {
				input = append(input, map[string]any{"type": "reasoning", "encrypted_content": value.Data, "summary": []any{}})
			}
		}
	}
	return input
}

func artifactText(artifacts []artifact.Artifact) string {
	var parts []string
	for _, art := range artifacts {
		if text, ok := art.(artifact.Text); ok && text.Content != "" {
			parts = append(parts, text.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func thinkingEffort(level models.ThinkingLevel) string {
	switch level {
	case models.ThinkingLevelMinimal:
		return "minimal"
	case models.ThinkingLevelLow:
		return "low"
	case models.ThinkingLevelMedium:
		return "medium"
	case models.ThinkingLevelHigh:
		return "high"
	case models.ThinkingLevelMax:
		return "xhigh"
	default:
		return ""
	}
}

type streamEvent struct {
	Type        string          `json:"type"`
	Delta       string          `json:"delta"`
	ItemID      string          `json:"item_id"`
	CallID      string          `json:"call_id"`
	OutputIndex int             `json:"output_index"`
	Item        json.RawMessage `json:"item"`
	Response    json.RawMessage `json:"response"`
	Error       *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type outputItem struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	EncryptedContent string `json:"encrypted_content"`
}

type responseDone struct {
	Status            string `json:"status"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage *struct {
		InputTokens       int `json:"input_tokens"`
		OutputTokens      int `json:"output_tokens"`
		TotalTokens       int `json:"total_tokens"`
		InputTokenDetails struct {
			CachedTokens     int `json:"cached_tokens"`
			CacheWriteTokens int `json:"cache_write_tokens"`
		} `json:"input_tokens_details"`
		OutputTokenDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func consumeSSE(ctx context.Context, body io.Reader, ch chan<- artifact.Artifact, span trace.Span) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var data strings.Builder
	state := streamState{}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				if err := handleEvent(ctx, []byte(data.String()), ch, &state, span); err != nil {
					return err
				}
				data.Reset()
				if state.completed {
					return nil
				}
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if data.Len() > 0 {
		if err := handleEvent(ctx, []byte(data.String()), ch, &state, span); err != nil {
			return err
		}
		if state.completed {
			return nil
		}
	}
	if !state.completed {
		return errors.New("stream closed before response.completed")
	}
	return nil
}

type streamState struct {
	toolUse   bool
	completed bool
	refusal   bool
	seenTool  map[string]bool
}

func handleEvent(ctx context.Context, data []byte, ch chan<- artifact.Artifact, state *streamState, span trace.Span) error {
	if string(data) == "[DONE]" {
		return nil
	}
	var event streamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("decode SSE event: %w", err)
	}
	switch event.Type {
	case "response.output_text.delta":
		return emit(ctx, ch, artifact.TextDelta{Content: event.Delta})
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return emit(ctx, ch, artifact.ReasoningDelta{Content: event.Delta})
	case "response.refusal.delta":
		state.refusal = true
		return emit(ctx, ch, artifact.TextDelta{Content: event.Delta})
	case "response.output_item.added":
		var item outputItem
		if json.Unmarshal(event.Item, &item) == nil && item.Type == "function_call" {
			state.toolUse = true
			state.markTool(item.ID, item.CallID)
			return emit(ctx, ch, artifact.ToolCallDelta{Index: event.OutputIndex, ID: first(item.CallID, item.ID), Name: item.Name, Arguments: item.Arguments})
		}
	case "response.function_call_arguments.delta":
		state.toolUse = true
		state.markTool(event.ItemID, event.CallID)
		return emit(ctx, ch, artifact.ToolCallDelta{Index: event.OutputIndex, ID: event.CallID, Arguments: event.Delta})
	case "response.output_item.done":
		var item outputItem
		if err := json.Unmarshal(event.Item, &item); err != nil {
			return fmt.Errorf("decode output item: %w", err)
		}
		if item.Type == "reasoning" && item.EncryptedContent != "" {
			return emit(ctx, ch, artifact.ReasoningSignature{Provider: "openai", SubKind: "encrypted", Data: item.EncryptedContent})
		}
		if item.Type == "function_call" {
			state.toolUse = true
			if !state.hasSeenTool(item.ID, item.CallID) {
				state.markTool(item.ID, item.CallID)
				return emit(ctx, ch, artifact.ToolCallDelta{Index: event.OutputIndex, ID: first(item.CallID, item.ID), Name: item.Name, Arguments: item.Arguments})
			}
		}
	case "response.completed", "response.done", "response.incomplete":
		var done responseDone
		completedPayload := event.Response
		if len(completedPayload) == 0 || string(completedPayload) == "null" {
			completedPayload = data
		}
		if err := json.Unmarshal(completedPayload, &done); err != nil {
			return fmt.Errorf("decode completed response: %w", err)
		}
		if done.Error != nil && done.Error.Message != "" {
			return errors.New(done.Error.Message)
		}
		reason := artifact.StopReasonStop
		if state.toolUse {
			reason = artifact.StopReasonToolUse
		}
		if state.refusal {
			reason = artifact.StopReasonRefusal
		}
		if (event.Type == "response.incomplete" || done.Status == "incomplete") && done.IncompleteDetails != nil && done.IncompleteDetails.Reason == "max_output_tokens" {
			reason = artifact.StopReasonLength
		}
		if err := emit(ctx, ch, artifact.StopReason{Reason: reason}); err != nil {
			return err
		}
		if done.Usage != nil {
			var thinking *int
			if done.Usage.OutputTokenDetails != nil {
				value := done.Usage.OutputTokenDetails.ReasoningTokens
				thinking = &value
			}
			usage := artifact.Usage{
				PromptTokens:     done.Usage.InputTokens,
				CompletionTokens: done.Usage.OutputTokens,
				TotalTokens:      done.Usage.TotalTokens,
				CacheReadTokens:  done.Usage.InputTokenDetails.CachedTokens,
				CacheWriteTokens: done.Usage.InputTokenDetails.CacheWriteTokens,
				ThinkingTokens:   thinking,
			}
			recordUsage(span, usage)
			state.completed = true
			return emit(ctx, ch, usage)
		}
		state.completed = true
	case "response.failed", "error":
		if event.Error != nil && event.Error.Message != "" {
			return errors.New(event.Error.Message)
		}
		if len(event.Response) > 0 {
			var failed responseDone
			if json.Unmarshal(event.Response, &failed) == nil && failed.Error != nil && failed.Error.Message != "" {
				return errors.New(failed.Error.Message)
			}
		}
		return errors.New("responses stream failed")
	}
	return nil
}

func recordUsage(span trace.Span, usage artifact.Usage) {
	if span == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.Int("gen_ai.usage.input_tokens", usage.PromptTokens),
		attribute.Int("gen_ai.usage.output_tokens", usage.CompletionTokens),
		attribute.Int("gen_ai.usage.total_tokens", usage.TotalTokens),
		attribute.Int("gen_ai.usage.cache_read.input_tokens", usage.CacheReadTokens),
		attribute.Int("gen_ai.usage.cache_creation.input_tokens", usage.CacheWriteTokens),
	}
	if usage.ThinkingTokens != nil {
		attrs = append(attrs, attribute.Int("gen_ai.usage.reasoning.output_tokens", *usage.ThinkingTokens))
	}
	span.SetAttributes(attrs...)
}

func (s *streamState) markTool(ids ...string) {
	if s.seenTool == nil {
		s.seenTool = make(map[string]bool)
	}
	for _, id := range ids {
		if id != "" {
			s.seenTool[id] = true
		}
	}
}

func (s *streamState) hasSeenTool(ids ...string) bool {
	for _, id := range ids {
		if id != "" && s.seenTool[id] {
			return true
		}
	}
	return false
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func emit(ctx context.Context, ch chan<- artifact.Artifact, value artifact.Artifact) error {
	select {
	case ch <- value:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type httpError struct {
	status int
	header http.Header
	body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("responses: upstream returned HTTP %d: %s", e.status, e.body)
}
func (e *httpError) StatusCode() int     { return e.status }
func (e *httpError) Header() http.Header { return e.header }

func newHTTPError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return &httpError{status: resp.StatusCode, header: resp.Header.Clone(), body: http.StatusText(resp.StatusCode)}
	}
	return &httpError{status: resp.StatusCode, header: resp.Header.Clone(), body: strings.TrimSpace(string(body))}
}

func (p *Provider) startSpan(ctx context.Context, model string) (context.Context, trace.Span) {
	if p.tracer == nil {
		return ctx, nil
	}
	ctx, span := p.tracer.Start(ctx, "provider.invoke", trace.WithSpanKind(trace.SpanKindClient))
	span.SetAttributes(attribute.String("model", model))
	if id, ok := loop.ThreadIDFrom(ctx); ok {
		span.SetAttributes(attribute.String("thread_id", id))
	}
	ctx = httptrace.WithClientTrace(ctx, otelhttptrace.NewClientTrace(ctx, otelhttptrace.WithoutSubSpans()))
	return ctx, span
}

func (p *Provider) recordError(span trace.Span, err error) {
	if span == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

var _ provider.Provider = (*Provider)(nil)
