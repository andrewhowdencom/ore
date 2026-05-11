package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Handler implements loop.Handler for executing tool calls.
// It looks up the tool by name in its registry, parses JSON arguments, executes
// the function, and appends a RoleTool turn with a ToolResult artifact.
type Handler struct {
	registry *Registry
}

// Compile-time interface check.
var _ loop.Handler = (*Handler)(nil)

// Handle processes a single artifact. If the artifact is not a ToolCall, it is
// ignored. For ToolCall artifacts, the handler looks up the tool in the
// registry, executes it, and appends the result (or an error) as a RoleTool
// turn with a ToolResult artifact.
func (h *Handler) Handle(ctx context.Context, art artifact.Artifact, s state.State) error {
	tc, ok := art.(artifact.ToolCall)
	if !ok {
		return nil
	}

	fn, ok := h.registry.lookup(tc.Name)
	if !ok {
		s.Append(state.RoleTool, artifact.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("tool %q not found", tc.Name),
			IsError:    true,
		})
		return nil
	}

	var args map[string]any
	if tc.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
			s.Append(state.RoleTool, artifact.ToolResult{
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("invalid tool arguments: %v", err),
				IsError:    true,
			})
			return nil
		}
	}

	result, err := fn(ctx, args)
	if err != nil {
		s.Append(state.RoleTool, artifact.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("tool execution error: %v", err),
			IsError:    true,
		})
		return nil
	}

	content, err := json.Marshal(result)
	if err != nil {
		s.Append(state.RoleTool, artifact.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("failed to serialize result: %v", err),
			IsError:    true,
		})
		return nil
	}

	s.Append(state.RoleTool, artifact.ToolResult{
		ToolCallID: tc.ID,
		Content:    string(content),
	})
	return nil
}
