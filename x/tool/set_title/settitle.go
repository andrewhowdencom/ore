// Package set_title provides a tool that allows an LLM to set the
// conversation title, which is broadcast to all conduits via
// PropertiesEvent.
//
// The tool name is set_title. When setting a title, prefer issue tracker IDs
// (e.g., #234, ABC-12345) if available; otherwise use the branch name;
// fallback to an arbitrary title.
//
// The package also exposes a slash command handler (Slash) that emits the
// same PropertiesEvent directly, so the /name slash command can update the
// conversation title without invoking a model or a tool-handler pipeline.
package set_title

import (
	"context"
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/slash"
)

// TitleUpdate carries the new conversation title. It implements
// artifact.StatusContributor so the tool handler broadcasts the title
// change to all session subscribers via PropertiesEvent.
type TitleUpdate struct{ Title string }

// Status returns the title as a map for PropertiesEvent.
func (t TitleUpdate) Status() map[string]string {
	return map[string]string{"title": t.Title}
}

// emitTitle validates raw input (non-empty after trimming) and returns the
// canonical title string. Both the tool-driven path (Tool) and the
// slash-driven path (Slash) use it to enforce the same input rules without
// duplicating the validation logic.
func emitTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", fmt.Errorf("missing or empty 'title' argument")
	}
	return title, nil
}

// Tool returns a ToolFunc that sets the conversation title.
// Expected argument: "title" (string). Prefer issue tracker IDs (e.g., #234,
// ABC-12345) if available; otherwise use the branch name; fallback to an
// arbitrary title.
func Tool() tool.ToolFunc {
	return func(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error) {
		raw, _ := args["title"].(string)
		title, err := emitTitle(raw)
		if err != nil {
			return nil, err
		}
		return TitleUpdate{Title: title}, nil
	}
}

// ToolDescriptor is the tool.Tool descriptor for the set_title
// tool. The slash command (Slash) operates independently of the
// tool layer; the descriptor is provided for callers that want
// to register set_title as a regular tool.Tool.
//
// The Format is populated with the framework default
// configuration (zero caps). The output of the tool is a single
// title string (carried by TitleUpdate), which is far smaller
// than the default 50 KB cap; the Format declaration is
// present for documentation and consistency.
var ToolDescriptor = tool.Tool{
	Name: "set_title",
	Description: "Set the conversation title. The title is broadcast to all " +
		"subscribers via PropertiesEvent. Prefer issue tracker IDs (e.g., #234, " +
		"ABC-12345) when available; otherwise use the branch name.\n\n" +
		"Output: a single title string (no truncation needed).",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "The conversation title to set.",
			},
		},
		"required": []string{"title"},
	},
	Format: tool.Format{
		// See AddTool in x/tool/calculator for rationale.
	},
}

// Slash returns a slash.Handler that sets the conversation title. The
// handler validates the command input (non-empty after trimming) and, when
// valid, emits a loop.PropertiesEvent directly so the slash path does not
// need a tool-handler pipeline. Empty or whitespace-only input is reported
// back to the user via Result.Notice with usage information and no
// PropertiesEvent is emitted.
func Slash() slash.Handler {
	return func(ctx context.Context, emitter loop.Emitter, cmd slash.Command) (slash.Result, error) {
		title, err := emitTitle(cmd.Input)
		if err != nil {
			return slash.Result{
				Notice: loop.Notice{Content: "Usage: /name <text>", Severity: loop.SeverityInfo},
			}, nil
		}
		emitter.Emit(ctx, loop.PropertiesEvent{
			Properties: map[string]string{"title": title},
			Ctx:        ctx,
		})
		return slash.Result{}, nil
	}
}

var (
	_ artifact.StatusContributor = TitleUpdate{}
)
