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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Handler implements loop.Handler for executing tool calls.
// It looks up the tool by name in its registry, parses JSON arguments, executes
// the function, and emits a TurnCompleteEvent with RoleTool and a ToolResult
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
// registry, executes it, and emits a TurnCompleteEvent with RoleTool and a
// ToolResult artifact.
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
			e.Emit(ctx, loop.TurnCompleteEvent{
				Turn: state.Turn{
					Role: state.RoleTool,
					Artifacts: []artifact.Artifact{artifact.ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("invalid tool arguments: %v", err),
						IsError:    true,
					}},
				},
			})
			return nil
		}
	}

	// Check for namespaced tool call (e.g., "filesystem/read_file")
	if namespace, name, ok := splitNamespace(tc.Name); ok {
		source := h.registry.LookupRemoteSource(namespace)
		if source == nil {
			e.Emit(ctx, loop.TurnCompleteEvent{
				Turn: state.Turn{
					Role: state.RoleTool,
					Artifacts: []artifact.Artifact{artifact.ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("tool namespace %q not found", namespace),
						IsError:    true,
					}},
				},
			})
			return nil
		}

		// Find remote tool descriptor for budget enforcement.
		var remoteTool toolpkg.Tool
		for _, t := range source.Tools() {
			if t.Name == name {
				remoteTool = t
				break
			}
		}

		result, err := source.Call(ctx, name, args)
		if err != nil {
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			content := fmt.Sprintf("tool execution error: %v", err)
			var value any
			if result != nil {
				if b, marshalErr := json.Marshal(result); marshalErr == nil {
					content = string(b)
					value = result
				}
			}
			e.Emit(ctx, loop.TurnCompleteEvent{
				Turn: state.Turn{
					Role: state.RoleTool,
					Artifacts: []artifact.Artifact{artifact.ToolResult{
						ToolCallID: tc.ID,
						Content:    toolpkg.TruncateContent(content, remoteTool.MaxBytes, len(content), remoteTool.TruncationHint),
						Value:      value,
						IsError:    true,
					}},
				},
			})
			return nil
		}

		content, err := json.Marshal(result)
		if err != nil {
			e.Emit(ctx, loop.TurnCompleteEvent{
				Turn: state.Turn{
					Role: state.RoleTool,
					Artifacts: []artifact.Artifact{artifact.ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("failed to serialize result: %v", err),
						IsError:    true,
					}},
				},
			})
			return nil
		}

		e.Emit(ctx, loop.TurnCompleteEvent{
			Turn: state.Turn{
				Role: state.RoleTool,
				Artifacts: []artifact.Artifact{artifact.ToolResult{
					ToolCallID: tc.ID,
					Content:    toolpkg.TruncateContent(string(content), remoteTool.MaxBytes, len(content), remoteTool.TruncationHint),
					Value:      result,
				}},
			},
		})
		return nil
	}

	// Local tool lookup
	toolDescriptor, fn, ok := h.registry.Lookup(tc.Name)
	if !ok {
		e.Emit(ctx, loop.TurnCompleteEvent{
			Turn: state.Turn{
				Role: state.RoleTool,
				Artifacts: []artifact.Artifact{artifact.ToolResult{
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("tool %q not found", tc.Name),
					IsError:    true,
				}},
			},
		})
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
		content := fmt.Sprintf("tool execution error: %v", err)
		var value any
		if result != nil {
			if b, marshalErr := json.Marshal(result); marshalErr == nil {
				content = string(b)
				value = result
			}
		}
		e.Emit(ctx, loop.TurnCompleteEvent{
			Turn: state.Turn{
				Role: state.RoleTool,
				Artifacts: []artifact.Artifact{artifact.ToolResult{
					ToolCallID: tc.ID,
					Content:    toolpkg.TruncateContent(content, toolDescriptor.MaxBytes, len(content), toolDescriptor.TruncationHint),
					Value:      value,
					IsError:    true,
				}},
			},
		})
		return nil
	}

	content, err := json.Marshal(result)
	if err != nil {
		e.Emit(ctx, loop.TurnCompleteEvent{
			Turn: state.Turn{
				Role: state.RoleTool,
				Artifacts: []artifact.Artifact{artifact.ToolResult{
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("failed to serialize result: %v", err),
					IsError:    true,
				}},
			},
		})
		return nil
	}

	e.Emit(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleTool,
			Artifacts: []artifact.Artifact{artifact.ToolResult{
				ToolCallID: tc.ID,
				Content:    toolpkg.TruncateContent(string(content), toolDescriptor.MaxBytes, len(content), toolDescriptor.TruncationHint),
				Value:      result,
			}},
		},
	})
	if sc, ok := result.(artifact.StatusContributor); ok {
		e.Emit(ctx, loop.PropertiesEvent{Properties: sc.Status()})
	}
	return nil
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
