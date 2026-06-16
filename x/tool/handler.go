package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	toolpkg "github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/tool/truncate"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Handler implements loop.Handler for executing tool calls.
// It looks up the tool by name in its registry, parses JSON arguments, executes
// the function, applies the tool's Format (truncation, recovery hint) to the
// result, and emits a TurnCompleteEvent with RoleTool and a ToolResult
// artifact.
type Handler struct {
	registry toolpkg.Registry
	tracer   trace.Tracer
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithTracer configures an OpenTelemetry tracer for the handler.
func WithTracer(tracer trace.Tracer) HandlerOption {
	return func(h *Handler) {
		h.tracer = tracer
	}
}

// NewHandler creates a Handler backed by the given registry.
func NewHandler(registry toolpkg.Registry, opts ...HandlerOption) *Handler {
	h := &Handler{registry: registry}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Compile-time interface check.
var _ loop.Handler = (*Handler)(nil)

// Handle processes a single artifact. If the artifact is not a ToolCall, it is
// ignored. For ToolCall artifacts, the handler looks up the tool in the
// registry, executes it, applies the tool's Format, and emits a
// TurnCompleteEvent with RoleTool and a ToolResult artifact.
func (h *Handler) Handle(ctx context.Context, art artifact.Artifact, e loop.Emitter) error {
	tc, ok := art.(artifact.ToolCall)
	if !ok {
		return nil
	}

	var span trace.Span
	if h.tracer != nil {
		ctx, span = h.tracer.Start(ctx, "tool.execute", trace.WithSpanKind(trace.SpanKindInternal))
		span.SetAttributes(attribute.String("tool.name", tc.Name))
		if id, ok := loop.ThreadIDFrom(ctx); ok {
			span.SetAttributes(attribute.String("thread_id", id))
		}
		defer span.End()
	}

	var args map[string]any
	if tc.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
			h.emitResult(ctx, e, tc.ID, fmt.Sprintf("invalid tool arguments: %v", err), nil, true, nil)
			return nil
		}
	}

	// Check for namespaced tool call (e.g., "filesystem/read_file")
	if namespace, name, ok := splitNamespace(tc.Name); ok {
		source := h.registry.LookupRemoteSource(namespace)
		if source == nil {
			h.emitResult(ctx, e, tc.ID, fmt.Sprintf("tool namespace %q not found", namespace), nil, true, nil)
			return nil
		}

		// Resolve the format once and share it across the success
		// and error branches. The error path renders the partial
		// result through the same format pipeline so Format
		// truncation and the LLMRenderer opt-out both work on
		// errors too.
		format := h.formatForRemote(source, name)

		result, err := source.Call(ctx, name, args)
		if err != nil {
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			h.handleError(ctx, e, tc.ID, err, result, format, span)
			return nil
		}

		// Look up the format from the remote source's Tool descriptor
		// if available. Remote sources may not carry a Format; in that
		// case the zero value is used and the framework default applies.
		content, trunc, err := h.applyFormat(ctx, result, format, span)
		if err != nil {
			h.emitResult(ctx, e, tc.ID, content, result, true, nil)
			return nil
		}
		h.emitResult(ctx, e, tc.ID, content, result, false, trunc)
		return nil
	}

	// Local tool lookup
	t, fn, ok := h.registry.Lookup(tc.Name)
	if !ok {
		h.emitResult(ctx, e, tc.ID, fmt.Sprintf("tool %q not found", tc.Name), nil, true, nil)
		return nil
	}

	// Resolve sandbox for this tool call. The handler checks three sources
	// in order: an explicit "sandbox" argument in the tool call, the
	// registry's default sandbox, or nil if the registry does not support
	// sandboxes. The "sandbox" key is removed from args so tools do not
	// see it in their argument map.
	var sb toolpkg.Sandbox
	if sbReg, ok := h.registry.(toolpkg.SandboxRegistry); ok {
		if name, ok := args["sandbox"].(string); ok {
			sb, _ = sbReg.LookupSandbox(name)
			delete(args, "sandbox")
		} else {
			sb = sbReg.DefaultSandbox()
		}
	}

	result, err := fn(ctx, sb, args)
	if err != nil {
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		h.handleError(ctx, e, tc.ID, err, result, t.Format, span)
		return nil
	}

	content, trunc, err := h.applyFormat(ctx, result, t.Format, span)
	if err != nil {
		h.emitResult(ctx, e, tc.ID, content, result, true, nil)
		// StatusContributor doesn't run for serialization errors.
		return nil
	}
	h.emitResult(ctx, e, tc.ID, content, result, false, trunc)

	// Preserve the existing StatusContributor contract: tools that
	// implement it broadcast ambient metadata to all subscribers.
	if sc, ok := result.(artifact.StatusContributor); ok {
		e.Emit(ctx, loop.PropertiesEvent{Properties: sc.Status()})
	}
	return nil
}

// formatForRemote looks up the Format from a remote source's Tool
// descriptors. The MCP package does not currently populate Format on its
// returned Tools, so the zero value (which triggers framework defaults) is
// the typical result. This function is the seam where future remote
// sources can opt into Format.
func (h *Handler) formatForRemote(source toolpkg.RemoteSource, name string) toolpkg.Format {
	for _, t := range source.Tools() {
		if t.Name == name {
			return t.Format
		}
	}
	return toolpkg.Format{}
}

// handleError centralizes the error path: it serializes whatever
// (err, result) pair the tool produced, applies the tool's Format
// (truncation, LLMRenderer opt-out) to the partial result if any, and
// emits the error ToolResult. Used by both the local and namespaced
// paths.
//
// The underlying err is always appended to Content as a
// `**Error:** <err.Error()>` footer. The footer is appended AFTER
// `applyFormat` returns, so it is exempt from the Format byte/line
// caps and is therefore never truncated. The LLM and the human both
// see the footer because ToolResult.LLMString and ToolResult.MarkdownString
// short-circuit to Content when IsError is true.
//
// When result is nil, a small synthetic value is built so the renderer
// pipeline still produces a structured body (rather than a blank line
// above the footer). The value is intentionally an anonymous struct
// to avoid leaking a public type just for this error path.
func (h *Handler) handleError(ctx context.Context, e loop.Emitter, toolCallID string, err error, result any, format toolpkg.Format, span trace.Span) {
	// Synthesize a value for the (nil, err) case so the renderer
	// pipeline produces a structured body above the footer.
	value := result
	if value == nil {
		value = struct {
			Error string `json:"error"`
		}{Error: err.Error()}
	}

	content, trunc, marshalErr := h.applyFormat(ctx, value, format, span)
	if marshalErr != nil {
		// Marshal failure is rare (unsupported types like channels).
		// Fall back to a small default body so the footer is still
		// appended and the LLM sees something structured.
		content = fmt.Sprintf("tool execution error: %v", err)
		trunc = nil
	}

	// Append the error footer AFTER truncation runs. The footer is
	// the most important thing the model sees after a failure, so it
	// is exempt from the byte/line caps by design.
	content = content + "\n\n**Error:** " + err.Error()

	h.emitResult(ctx, e, toolCallID, content, result, true, trunc)
}

// applyFormat renders the LLM-facing string for a tool result,
// respecting the tool's Format declaration. The flow:
//
//  1. If result is nil, return empty content.
//  2. If result implements artifact.LLMRenderer, use MarshalLLM() as-is
//     (explicit opt-out — the framework does NOT truncate LLMRenderer
//     output). This is what tools with custom rendering (e.g. bash
//     with a temp-file fallback) use to take full control.
//  3. Otherwise, JSON-marshal the value and apply the tool's Format
//     (truncation + recovery hint).
//
// The returned error is non-nil only when the result is non-nil but
// could not be JSON-serialized. This is a programming error in the
// tool (returning an unsupported type like a channel). The caller
// should surface it as a ToolResult with IsError=true.
func (h *Handler) applyFormat(ctx context.Context, result any, format toolpkg.Format, span trace.Span) (string, *artifact.Truncation, error) {
	if result == nil {
		return "", nil, nil
	}

	// Step 2: LLMRenderer opt-out.
	if r, ok := result.(artifact.LLMRenderer); ok {
		return r.MarshalLLM(), nil, nil
	}

	// Step 3: JSON-marshal then truncate.
	b, err := json.Marshal(result)
	if err != nil {
		// Marshal failure is a programming error in the tool.
		// Return a small, distinct error message; the caller will
		// mark the result as IsError=true.
		return fmt.Sprintf("failed to serialize result: %v", err), nil, err
	}

	tr := h.applyTruncationToString(ctx, string(b), format, span)
	return tr.content, tr.truncation, nil
}

// truncationResult is the internal pair returned by applyTruncationToString.
type truncationResult struct {
	content     string
	truncation  *artifact.Truncation
}

// applyTruncationToString runs the truncator over a pre-rendered string
// and, if truncation occurred, appends the rendered recovery hint and a
// "X lines shown of Y total" notice. The truncator is invoked with the
// tool's Format; zero values trigger the framework defaults.
func (h *Handler) applyTruncationToString(ctx context.Context, s string, format toolpkg.Format, span trace.Span) truncationResult {
	cfg := format.ResolvedTruncateConfig()
	style := format.Style
	if style == 0 {
		style = toolpkg.StyleTail
	}
	out, trunc := truncate.Truncate(s, cfg, style)

	if span != nil && trunc.Truncated() {
		span.SetAttributes(
			attribute.Bool("tool.truncated", true),
			attribute.Int("tool.truncation.original_bytes", trunc.OriginalBytes),
			attribute.Int("tool.truncation.shown_bytes", trunc.ShownBytes),
			attribute.Int("tool.truncation.original_lines", trunc.OriginalLines),
			attribute.Int("tool.truncation.shown_lines", trunc.ShownLines),
			attribute.String("tool.truncation.style", trunc.Style),
		)
	}

	if !trunc.Truncated() {
		return truncationResult{content: out, truncation: nil}
	}

	if format.RecoveryHint != "" {
		rendered := truncate.RenderHint(format.RecoveryHint, trunc)
		if rendered != "" {
			out = out + "\n\n" + rendered
		}
		out = out + fmt.Sprintf("\n[%d lines shown of %d total]", trunc.ShownLines, trunc.OriginalLines)
	}
	return truncationResult{content: out, truncation: &trunc}
}

// emitResult is the single emit point for ToolResults. Centralizing
// the construction here means the truncation-aware and the
// non-truncation paths share one code path; future changes to
// ToolResult serialization only need to be made once.
func (h *Handler) emitResult(ctx context.Context, e loop.Emitter, toolCallID, content string, value any, isError bool, trunc *artifact.Truncation) {
	e.Emit(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleTool,
			Artifacts: []artifact.Artifact{artifact.ToolResult{
				ToolCallID: toolCallID,
				Content:    content,
				Value:      value,
				IsError:    isError,
				Truncation: trunc,
			}},
		},
	})
}

// splitNamespace splits a namespaced tool name into its namespace and tool
// name components. It returns ok=false if the name is not namespaced.
func splitNamespace(name string) (namespace, toolName string, ok bool) {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
