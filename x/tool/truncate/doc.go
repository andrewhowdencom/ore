// Package truncate implements the default byte- and line-bounded
// truncation for tool results, plus a small {name} template
// substitution helper for rendering recovery hints.
//
// The truncator is the framework's default implementation of the
// tool.Format contract. The framework handler in x/tool applies it
// after every tool execution, unless the tool returns a value
// implementing artifact.LLMRenderer (the explicit opt-out).
//
// Algorithm
//
// Truncate respects both a byte cap and a line cap. The smaller of
// the two kept lengths wins. Cuts are made at UTF-8 rune boundaries
// so the returned string is always valid UTF-8 even when the byte
// cap falls in the middle of a multi-byte rune. Line caps count
// "\n" characters in the kept portion; a trailing line without a
// newline is dropped because it is partial.
//
// Style controls whether the kept portion is the head (start) or
// tail (end) of the input. The zero value of tool.StyleTail
// matches terminal-output conventions, where errors and summaries
// live at the end.
//
// Recovery hints
//
// RenderHint substitutes {name} placeholders in a template string
// with values from a Truncation descriptor. Unknown placeholders
// are left as-is. This is intentionally simpler than
// text/template: the placeholder set is small and fixed, and a
// missing field should not cause the whole result to fail to
// render.
//
// Usage
//
//	import "github.com/andrewhowdencom/ore/x/tool/truncate"
//	import "github.com/andrewhowdencom/ore/tool"
//
//	out, trunc := truncate.Truncate(s, tool.Format{Truncate: tool.TruncateConfig{MaxBytes: 1024}})
//	hint := truncate.RenderHint("Use offset={next_offset} to continue.", trunc)
package truncate
