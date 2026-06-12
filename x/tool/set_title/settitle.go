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

// Slash returns a slash.Handler that sets the conversation title. The
// handler validates the command input (non-empty after trimming) and, when
// valid, emits a loop.PropertiesEvent directly so the slash path does not
// need a tool-handler pipeline. Empty or whitespace-only input is reported
// back to the user via Result.Feedback with usage information and no
// PropertiesEvent is emitted.
func Slash() slash.Handler {
	return func(ctx context.Context, emitter loop.Emitter, cmd slash.Command) (slash.Result, error) {
		title, err := emitTitle(cmd.Input)
		if err != nil {
			return slash.Result{
				Feedback: artifact.Text{Content: "Usage: /name <text>"},
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
