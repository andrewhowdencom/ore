package tool

import "context"

// ToolFunc is a callable tool implementation. It receives parsed JSON arguments
// as a map and returns any result value, which is JSON-serialized before being
// sent back to the LLM.
type ToolFunc func(ctx context.Context, args map[string]any) (any, error)
