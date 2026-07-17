// Package analytics provides tools for analyzing the statistics of
// conversation state, such as per-(kind, source) counts and byte
// sizes. Source identifies the originating tool for tool_call and
// tool_result artifacts, allowing callers to attribute context cost
// to specific tools.
//
// For tool_result artifacts, Source is resolved by joining the
// result's ToolCallID against the originating tool_call.Name in the
// same scope (the slice the caller passes to AnalyzeTurns, or the
// load function's returns to AnalyzeStore). When no matching
// tool_call exists in the scope (e.g. compaction has dropped the
// call), the result buckets under the Source "(unknown)" so the gap
// is visible in the report.
//
// The package depends only on artifact, ledger, and x/llmbytes: it
// takes plain values from any thread-like container and never
// imports session, junk, or any storage abstraction. Multi-store
// aggregation is the caller's responsibility — pass a load function
// to AnalyzeStore and let it enumerate threads however it likes.
//
// The Render function turns a []Stats into a Markdown table
// (header, per-bucket rows, bolded totals row) suitable for
// surfacing in a chat reply or TUI feedback message.
package analytics