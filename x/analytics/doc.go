// Package analytics provides tools for analyzing the statistics of
// conversation state, such as per-(kind, source) counts and byte
// sizes. Source identifies the originating tool for tool_call and
// tool_result artifacts, allowing callers to attribute context cost
// to specific tools.
//
// For tool_result artifacts, Source is resolved by joining the
// result's ToolCallID against the originating tool_call.Name in the
// same turn. When no matching tool_call exists in the same turn
// (e.g. compaction has dropped the call), the result buckets under
// the Source "(unknown)" so the gap is visible in the report.
package analytics
