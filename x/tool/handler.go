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
)

// Handler implements loop.Handler for executing tool calls.
// It looks up the tool by name in its registry, parses JSON arguments, executes
// the function, and emits a TurnCompleteEvent with RoleTool and a ToolResult
// artifact.
type Handler struct {
	registry toolpkg.Registry
}

// NewHandler creates a Handler backed by the given registry.
func NewHandler(registry toolpkg.Registry) *Handler {
	return &Handler{registry: registry}
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

		result, err := source.Call(ctx, name, args)
		if err != nil {
			e.Emit(ctx, loop.TurnCompleteEvent{
				Turn: state.Turn{
					Role: state.RoleTool,
					Artifacts: []artifact.Artifact{artifact.ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("tool execution error: %v", err),
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
					Content:    string(content),
					Value:      result,
				}},
			},
		})
		return nil
	}

	// Local tool lookup
	fn, ok := h.registry.Lookup(tc.Name)
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
		e.Emit(ctx, loop.TurnCompleteEvent{
			Turn: state.Turn{
				Role: state.RoleTool,
				Artifacts: []artifact.Artifact{artifact.ToolResult{
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("tool execution error: %v", err),
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
				Content:    string(content),
				Value:      result,
			}},
		},
	})
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
