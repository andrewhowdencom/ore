// Package export renders junk.Thread conversation histories into
// human-reviewable formats: plain text, self-contained HTML, and JSON.
//
// All three top-level functions accept an io.Writer and a *junk.Thread,
// iterate over the thread's turns, and emit every artifact in the turn.
// Delta artifacts are never present in persisted threads so no special
// handling is required.
//
// HTML rendering specifics:
//
//   - Text artifact content is rendered as markdown (CommonMark plus
//     the GFM extension for tables, strikethrough, task lists, and
//     autolinks) and sanitized via bluemonday.UGCPolicy. LLM-emitted
//     <script>, <iframe>, and javascript: URLs are stripped.
//   - Reasoning artifacts collapse under a <details> element. Their
//     content is rendered as escaped plaintext, not markdown, because
//     reasoning text is typically stream-of-thought prose.
//   - Each ToolCall and its matching ToolResult in the same turn
//     collapse under a single <details> element, with the tool name as
//     the summary. A ToolResult without a same-turn pair renders as a
//     standalone collapsible.
//   - Truncated tool results (Truncation != nil && Truncated())
//     surface a meta line below the result body stating the byte and
//     line counts and the truncation style.
//   - The output is a single self-contained HTML document with inline
//     CSS and no JavaScript; it renders correctly in browsers without
//     script execution and in most email clients.
package export