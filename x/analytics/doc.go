// Package analytics provides tools for analyzing the statistics of
// conversation state, such as per-(kind, source) counts and byte
// sizes. Source identifies the originating tool for tool_call and
// tool_result artifacts, allowing callers to attribute context cost
// to specific tools.
package analytics
