// Package set_title provides a tool that allows an LLM to set the
// conversation title, which is broadcast to all conduits via
// PropertiesEvent.
//
// The tool name is set_title. When setting a title, prefer issue tracker IDs
// (e.g., #234, ABC-12345) if available; otherwise use the branch name;
// fallback to an arbitrary title.
package set_title

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/tool"
)

// TitleUpdate carries the new conversation title. It implements
// artifact.StatusContributor so the tool handler broadcasts the title
// change to all session subscribers via PropertiesEvent.
type TitleUpdate struct{ Title string }

// Status returns the title as a map for PropertiesEvent.
func (t TitleUpdate) Status() map[string]string {
	return map[string]string{"title": t.Title}
}

// Tool returns a ToolFunc that sets the conversation title.
// Expected argument: "title" (string). Prefer issue tracker IDs (e.g., #234,
// ABC-12345) if available; otherwise use the branch name; fallback to an
// arbitrary title.
func Tool() tool.ToolFunc {
	return func(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error) {
		title, _ := args["title"].(string)
		if title == "" {
			return nil, fmt.Errorf("missing or empty 'title' argument")
		}
		return TitleUpdate{Title: title}, nil
	}
}

var (
	_ artifact.StatusContributor = TitleUpdate{}
)
