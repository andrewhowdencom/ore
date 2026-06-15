package filesystem

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/tool/truncate"
)

// Compile-time type checks.
var (
	_ tool.ToolFunc = ReadFile
	_ tool.ToolFunc = WriteFile
	_ tool.ToolFunc = EditFile
	_ tool.ToolFunc = ListDirectory
	_ tool.ToolFunc = SearchFiles
)

// frameworkDefaultByteCap is the default per-call byte cap
// applied to the LLM-facing result. Mirrors tool.FrameworkDefaultMaxBytes.
const frameworkDefaultByteCap = 50_000

// frameworkDefaultMaxRows is the default per-call row cap for
// list_directory (500) and search_files (1000).
const (
	frameworkDefaultListRows = 500
	frameworkDefaultSearchRows = 1000
)

// resolvePath resolves a path through a sandbox when available. If sb is nil
// or does not implement FileSandbox, the path is returned as-is (allowing
// absolute paths). If sb implements FileSandbox, all paths are delegated
// to fsb.ResolvePath, letting the sandbox decide whether to reject,
// rewrite, or pass through absolute paths.
func resolvePath(sb tool.Sandbox, path string) (string, error) {
	if sb == nil {
		return path, nil
	}
	fsb, ok := sb.(tool.FileSandbox)
	if !ok {
		return path, nil
	}
	return fsb.ResolvePath(path)
}

// ReadFileResult carries the bounded line-numbered content of a
// read_file call, plus optional truncation metadata and the path
// to a temp file that holds the full file content when the LLM-facing
// string was truncated.
type ReadFileResult struct {
	// Content is the line-numbered, possibly-truncated file
	// content. Always present; empty if the file is empty or
	// the offset is past EOF.
	Content string

	// TempFilePath is the path to a temp file holding the full
	// file content, set when truncation occurred. Empty when
	// no truncation occurred.
	TempFilePath string

	// Truncation is non-nil when the LLM-facing content was
	// truncated. The Truncation.OriginalBytes reflects the
	// byte length of the line-numbered output as produced by
	// the reader (i.e., the size of the bounded string the
	// user would have seen if no cap was in effect).
	Truncation *artifact.Truncation
}

// MarshalLLM returns the LLM-facing string representation. The
// base content is the bounded, line-numbered text. When
// truncation occurred, a recovery hint is appended that names the
// temp file and instructs the model to use offset={next_offset}.
func (r *ReadFileResult) MarshalLLM() string {
	if r.Truncation == nil || !r.Truncation.Truncated() {
		return r.Content
	}
	hint := truncate.RenderHint(
		ReadFileTool.Format.RecoveryHint,
		*r.Truncation,
		map[string]string{
			"path":     r.TempFilePath,
			"next_offset": fmt.Sprintf("%d", r.Truncation.ShownLines+1),
		},
	)
	var sb strings.Builder
	sb.WriteString(r.Content)
	if hint != "" {
		sb.WriteString("\n\n")
		sb.WriteString(hint)
	}
	sb.WriteString(fmt.Sprintf("\n[%d lines shown of %d total; full content at %s]",
		r.Truncation.ShownLines, r.Truncation.OriginalLines, r.TempFilePath))
	return sb.String()
}

// MarshalMarkdown returns the Markdown representation of the
// result for human display (e.g. the TUI). The line-numbered
// content is wrapped in a fenced code block so glamour preserves
// the line breaks and (when a language is recognised) syntax
// highlights the contents. When truncation occurred, a short
// recovery hint naming the temp file path is appended below the
// code block — a Markdown link keeps the path clickable in
// downstream Markdown renderers.
func (r *ReadFileResult) MarshalMarkdown() string {
	var sb strings.Builder
	sb.WriteString("```\n")
	sb.WriteString(r.Content)
	if !strings.HasSuffix(r.Content, "\n") && r.Content != "" {
		sb.WriteString("\n")
	}
	sb.WriteString("```")
	if r.Truncation != nil && r.Truncation.Truncated() && r.TempFilePath != "" {
		sb.WriteString(fmt.Sprintf(
			"\n\nOutput truncated (%d of %d lines shown). Full file: `%s`. Use offset=%d to continue.",
			r.Truncation.ShownLines,
			r.Truncation.OriginalLines,
			r.TempFilePath,
			r.Truncation.ShownLines+1,
		))
	}
	return sb.String()
}

// Compile-time assertion: *ReadFileResult implements both the
// LLM and Markdown renderers so the framework handler keeps the
// line-numbered output verbatim for the LLM, and the TUI
// renders it as a code block.
var (
	_ artifact.LLMRenderer     = (*ReadFileResult)(nil)
	_ artifact.MarkdownRenderer = (*ReadFileResult)(nil)
)

// ReadFile reads a file and returns its line-numbered content
// with byte-cap truncation and temp-file fallback. When the
// line-numbered output exceeds the cap, the full file is written
// to a temp file and the result's TempFilePath points to it; the
// LLM can read the temp file to retrieve the rest.
//
// Parameters:
//   - path (string, required): relative or absolute file path.
//   - offset (number, optional, default 1): 1-based starting line.
//   - limit  (number, optional, default 0): maximum lines to return (0 = no limit).
func ReadFile(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	resolved, err := resolvePath(sb, path)
	if err != nil {
		return nil, err
	}
	path = resolved

	offset := toInt(args["offset"], 1)
	if offset < 1 {
		offset = 1
	}
	limit := toInt(args["limit"], 0)

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path %q is a directory", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var result strings.Builder
	lineNum := 0
	linesEmitted := 0

	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}

		lineNum++

		if !utf8.ValidString(line) {
			return nil, fmt.Errorf("cannot read binary file %s (%.1f MB): invalid UTF-8 detected", path, float64(info.Size())/(1024*1024))
		}

		if readErr == io.EOF && line == "" {
			break
		}

		if lineNum < offset {
			if readErr == io.EOF {
				break
			}
			continue
		}

		if limit > 0 && linesEmitted >= limit {
			break
		}

		formatted := fmt.Sprintf("%d|%s\n", lineNum, line)
		result.WriteString(formatted)
		linesEmitted++

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("failed to read file: %w", readErr)
		}
	}

	full := result.String()
	res := &ReadFileResult{Content: full}

	// Apply the tool's Format cap. If the result is small, this
	// is a no-op. If it exceeds the cap, the line-numbered output
	// is truncated and a temp file is written with the FULL file
	// content (so the model can re-read the parts that were
	// truncated).
	cfg := ReadFileTool.Format.ResolvedTruncateConfig()
	style := ReadFileTool.Format.Style
	if style == 0 {
		style = tool.StyleHead
	}
	out, trunc := truncate.Truncate(full, cfg, style)
	if trunc.Truncated() {
		tempPath, ferr := writeFullFileToTemp(path)
		if ferr == nil {
			res.TempFilePath = tempPath
		}
		// Even if the temp-file write fails, surface the
		// truncation so the model knows the result was bounded.
		res.Content = out
		res.Truncation = &trunc
	}

	return res, nil
}

// writeFullFileToTemp copies the full contents of path to a
// freshly created temp file. The temp file is created via
// os.CreateTemp and is not removed automatically; the caller
// passes the path back to the LLM in the recovery hint and the
// LLM (or a follow-up call) cleans it up.
func writeFullFileToTemp(path string) (string, error) {
	src, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	dst, err := os.CreateTemp("", "ore-readfile-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	// Best-effort close; we capture errors below.
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(dst.Name())
		return "", fmt.Errorf("copy: %w", err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(dst.Name())
		return "", fmt.Errorf("close temp: %w", err)
	}
	return dst.Name(), nil
}

// ReadFileTool is the tool.Tool descriptor for ReadFile.
var ReadFileTool = tool.Tool{
	Name: "read_file",
	Description: "Read the contents of a file. Returns line-number-prefixed content.\n\n" +
		"Output limits: when the line-numbered output exceeds 50 KB / 2000 " +
		"lines, the full file is written to a temp file and only the head " +
		"of the line-numbered content is returned.\n\n" +
		"Recovery: when truncation occurs, the result includes the temp file " +
		"path. Read the temp file with read_file to access the full content, " +
		"or use a more specific offset/limit.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The relative or absolute path to the file to read.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "The 1-based line number to start reading from. Defaults to 1.",
				"default":     1,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "The maximum number of lines to return. 0 means no limit.",
				"default":     0,
			},
		},
		"required": []string{"path"},
	},
	DisplayHint: func(args map[string]any) any {
		return fmt.Sprintf("📄 read_file(%s)", toString(args["path"]))
	},
	Format: tool.Format{
		Truncate: tool.TruncateConfig{
			MaxBytes: frameworkDefaultByteCap,
			MaxLines: 2000,
		},
		Style: tool.StyleHead,
		RecoveryHint: "Output truncated. Use offset={next_offset} to continue, or read the full file at {path}.",
	},
}

// WriteFileResult carries the acknowledgement returned by
// WriteFile. The Message field holds the LLM-facing string
// (e.g. `wrote 42 bytes to "/foo"`). The type implements both
// artifact.LLMRenderer and artifact.MarkdownRenderer so the
// TUI can render the acknowledgement cleanly (as a fenced
// code block) instead of the JSON-shaped noise that
// json.Marshal(string) would produce.
type WriteFileResult struct {
	Path    string
	Bytes   int
	Message string
}

// MarshalLLM returns the LLM-facing acknowledgement.
func (r *WriteFileResult) MarshalLLM() string { return r.Message }

// MarshalMarkdown returns the Markdown representation of the
// acknowledgement for human display. The ack is wrapped in a
// fenced code block so glamour preserves the formatting
// (otherwise a bare `wrote N bytes to "path"` string is
// JSON-marshaled by the framework's fallback path and rendered
// with literal quote characters and escape sequences).
func (r *WriteFileResult) MarshalMarkdown() string {
	var sb strings.Builder
	sb.WriteString("```\n")
	if r.Message != "" {
		sb.WriteString(r.Message)
		sb.WriteString("\n")
	}
	sb.WriteString("```")
	return sb.String()
}

var (
	_ artifact.LLMRenderer     = (*WriteFileResult)(nil)
	_ artifact.MarkdownRenderer = (*WriteFileResult)(nil)
)

// WriteFile creates a new file with the given content, overwriting if it exists.
// Parameters:
//   - path    (string, required): relative or absolute file path.
//   - content (string, required): file contents to write.
func WriteFile(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	resolved, err := resolvePath(sb, path)
	if err != nil {
		return nil, err
	}
	path = resolved

	content := toString(args["content"])

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create parent directories: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return &WriteFileResult{
		Path:    path,
		Bytes:   len(content),
		Message: fmt.Sprintf("wrote %d bytes to %q", len(content), path),
	}, nil
}

// WriteFileTool is the tool.Tool descriptor for WriteFile.
var WriteFileTool = tool.Tool{
	Name:        "write_file",
	Description: "Create or overwrite a file with the specified content. Returns a short acknowledgement.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The relative or absolute path to the file to create.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write into the file.",
			},
		},
		"required": []string{"path", "content"},
	},
	DisplayHint: func(args map[string]any) any {
		return fmt.Sprintf("📝 write_file(%s)", toString(args["path"]))
	},
}

// EditFileResult carries the acknowledgement returned by
// EditFile. Like WriteFileResult, the type implements both
// LLMRenderer and MarkdownRenderer so the TUI renders the ack
// as a clean fenced code block rather than JSON-quoted noise.
type EditFileResult struct {
	Path    string
	Message string
}

// MarshalLLM returns the LLM-facing acknowledgement.
func (r *EditFileResult) MarshalLLM() string { return r.Message }

// MarshalMarkdown returns the Markdown representation of the
// acknowledgement for human display. See WriteFileResult for the
// rationale behind wrapping the message in a code fence.
func (r *EditFileResult) MarshalMarkdown() string {
	var sb strings.Builder
	sb.WriteString("```\n")
	if r.Message != "" {
		sb.WriteString(r.Message)
		sb.WriteString("\n")
	}
	sb.WriteString("```")
	return sb.String()
}

var (
	_ artifact.LLMRenderer     = (*EditFileResult)(nil)
	_ artifact.MarkdownRenderer = (*EditFileResult)(nil)
)

// EditFile performs an exact-match search-and-replace on an existing file.
// It replaces the first occurrence of old_string with new_string.
// Parameters:
//   - path       (string, required): relative or absolute file path.
//   - old_string (string, required): exact text to search for.
//   - new_string (string, required): replacement text.
func EditFile(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	resolved, err := resolvePath(sb, path)
	if err != nil {
		return nil, err
	}
	path = resolved

	oldStr := toString(args["old_string"])
	if oldStr == "" {
		return nil, fmt.Errorf("old_string cannot be empty")
	}
	newStr := toString(args["new_string"])

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)
	idx := strings.Index(content, oldStr)
	if idx == -1 {
		return nil, fmt.Errorf("old_string not found in %q", path)
	}

	updated := content[:idx] + newStr + content[idx+len(oldStr):]
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return &EditFileResult{
		Path:    path,
		Message: fmt.Sprintf("edited %q", path),
	}, nil
}

// EditFileTool is the tool.Tool descriptor for EditFile.
var EditFileTool = tool.Tool{
	Name:        "edit_file",
	Description: "Edit an existing file by replacing the first exact occurrence of old_string with new_string. Fails if old_string is empty or not found. Returns a short acknowledgement.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The relative or absolute path to the file to edit.",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The exact text to search for in the file. Must match literally (case-sensitive).",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The replacement text to insert in place of old_string.",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	},
	DisplayHint: func(args map[string]any) any {
		return fmt.Sprintf("✏️ edit_file(%s)", toString(args["path"]))
	},
}

// ListDirectoryResult carries the bounded entry list from a
// list_directory call, plus optional truncation metadata.
type ListDirectoryResult struct {
	Entries    []string
	Truncation *artifact.Truncation
}

// MarshalLLM returns the LLM-facing string representation. The
// base content is a newline-separated list of entries. When
// truncation occurred, a recovery hint is appended that
// recommends a higher limit.
func (r *ListDirectoryResult) MarshalLLM() string {
	if r.Truncation == nil || !r.Truncation.Truncated() {
		return strings.Join(r.Entries, "\n")
	}
	hint := truncate.RenderHint(
		ListDirectoryTool.Format.RecoveryHint,
		*r.Truncation,
	)
	var sb strings.Builder
	sb.WriteString(strings.Join(r.Entries, "\n"))
	if hint != "" {
		sb.WriteString("\n\n")
		sb.WriteString(hint)
	}
	sb.WriteString(fmt.Sprintf("\n[%d entries shown of %d total]",
		r.Truncation.ShownLines, r.Truncation.OriginalLines))
	return sb.String()
}

// MarshalMarkdown returns the Markdown representation of the
// result for human display. The entry list is wrapped in a
// fenced code block so glamour preserves the newlines between
// names. When truncation occurred, a short recovery hint
// follows the code block.
func (r *ListDirectoryResult) MarshalMarkdown() string {
	var sb strings.Builder
	sb.WriteString("```\n")
	sb.WriteString(strings.Join(r.Entries, "\n"))
	if len(r.Entries) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString("```")
	if r.Truncation != nil && r.Truncation.Truncated() {
		sb.WriteString(fmt.Sprintf(
			"\n\nOutput truncated (%d of %d entries shown). Use a higher `limit` to see more.",
			r.Truncation.ShownLines,
			r.Truncation.OriginalLines,
		))
	}
	return sb.String()
}

var (
	_ artifact.LLMRenderer     = (*ListDirectoryResult)(nil)
	_ artifact.MarkdownRenderer = (*ListDirectoryResult)(nil)
)

// ListDirectory returns a shallow listing of non-hidden entries in a directory.
// Parameters:
//   - path  (string, required): relative or absolute directory path.
//   - limit (number, optional, default 500): maximum entries to return.
func ListDirectory(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	resolved, err := resolvePath(sb, path)
	if err != nil {
		return nil, err
	}
	path = resolved

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path %q is not a directory", path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	all := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		all = append(all, name)
	}

	limit := toInt(args["limit"], frameworkDefaultListRows)
	res := &ListDirectoryResult{}
	if limit > 0 && len(all) > limit {
		res.Entries = all[:limit]
		res.Truncation = &artifact.Truncation{
			OriginalBytes: len(strings.Join(all, "\n")),
			OriginalLines: len(all),
			ShownBytes:    len(strings.Join(res.Entries, "\n")),
			ShownLines:    len(res.Entries),
			Style:         "head",
		}
	} else {
		res.Entries = all
	}

	return res, nil
}

// ListDirectoryTool is the tool.Tool descriptor for ListDirectory.
var ListDirectoryTool = tool.Tool{
	Name:        "list_directory",
	Description: "List the immediate non-hidden entries in a directory. Returns entry names. " +
		"Hidden entries (names starting with '.') are excluded.\n\n" +
		"Output limits: capped at 500 entries by default. Use the limit " +
		"parameter to control the cap. When truncated, the result includes a " +
		"hint to use a higher limit to see more.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The relative or absolute path to the directory to list.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of entries to return. Defaults to 500.",
				"default":     frameworkDefaultListRows,
			},
		},
		"required": []string{"path"},
	},
	DisplayHint: func(args map[string]any) any {
		return fmt.Sprintf("📁 list_directory(%s)", toString(args["path"]))
	},
	Format: tool.Format{
		Truncate: tool.TruncateConfig{
			MaxLines: frameworkDefaultListRows,
		},
		Style:        tool.StyleHead,
		RecoveryHint: "Output truncated. Use limit=2N to see more entries, or limit=N to reduce.",
	},
}

// SearchResult represents a single regex match found by SearchFiles.
// It is serialized to JSON with fields:
//   - path: the file path where the match was found,
//   - line_number: the 1-based line index of the match,
//   - content: the matching line text.
type SearchResult struct {
	Path       string `json:"path"`
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
}

// SearchFilesResult carries the bounded match list from a
// search_files call, plus optional truncation metadata.
type SearchFilesResult struct {
	Results    []SearchResult
	Truncation *artifact.Truncation
}

// MarshalLLM returns the LLM-facing string representation. The
// base content is one line per match ("path:line: content"). When
// truncation occurred, a recovery hint is appended recommending
// a higher limit or a more specific search.
func (r *SearchFilesResult) MarshalLLM() string {
	var sb strings.Builder
	for _, m := range r.Results {
		sb.WriteString(fmt.Sprintf("%s:%d: %s\n", m.Path, m.LineNumber, m.Content))
	}
	if r.Truncation == nil || !r.Truncation.Truncated() {
		return sb.String()
	}
	hint := truncate.RenderHint(
		SearchFilesTool.Format.RecoveryHint,
		*r.Truncation,
	)
	if hint != "" {
		sb.WriteString("\n")
		sb.WriteString(hint)
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("[%d matches shown of %d total]",
		r.Truncation.ShownLines, r.Truncation.OriginalLines))
	return sb.String()
}

// MarshalMarkdown returns the Markdown representation of the
// result for human display. Each match is wrapped in a fenced
// code block so the "path:line: content" framing is preserved
// verbatim (no soft-wrapping that would obscure the location).
// When truncation occurred, a short recovery hint follows.
func (r *SearchFilesResult) MarshalMarkdown() string {
	var sb strings.Builder
	sb.WriteString("```\n")
	for _, m := range r.Results {
		fmt.Fprintf(&sb, "%s:%d: %s\n", m.Path, m.LineNumber, m.Content)
	}
	sb.WriteString("```")
	if r.Truncation != nil && r.Truncation.Truncated() {
		sb.WriteString(fmt.Sprintf(
			"\n\nOutput truncated (%d of %d matches shown). Use a higher `limit` or refine the regex.",
			r.Truncation.ShownLines,
			r.Truncation.OriginalLines,
		))
	}
	return sb.String()
}

var (
	_ artifact.LLMRenderer     = (*SearchFilesResult)(nil)
	_ artifact.MarkdownRenderer = (*SearchFilesResult)(nil)
)

// SearchFiles searches files for lines matching a regex query.
// Parameters:
//   - path  (string, required): file or directory path to search.
//   - query (string, required): regex pattern to match.
//   - limit (number, optional, default 1000): maximum matches to return.
func SearchFiles(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	resolved, err := resolvePath(sb, path)
	if err != nil {
		return nil, err
	}
	path = resolved

	query := toString(args["query"])
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	re, err := regexp.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	results := make([]SearchResult, 0)

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	if !info.IsDir() {
		matches, err := searchFile(path, re)
		if err != nil {
			return nil, err
		}
		results = append(results, matches...)
	} else {
		err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // Skip entries we cannot read.
			}
			if d.IsDir() {
				if strings.HasPrefix(filepath.Base(p), ".") && p != path {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(filepath.Base(p), ".") {
				return nil
			}
			matches, err := searchFile(p, re)
			if err != nil {
				return nil // Skip files we cannot read.
			}
			results = append(results, matches...)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to walk directory: %w", err)
		}
	}

	limit := toInt(args["limit"], frameworkDefaultSearchRows)
	res := &SearchFilesResult{}
	if limit > 0 && len(results) > limit {
		res.Results = results[:limit]
		res.Truncation = &artifact.Truncation{
			OriginalBytes: countResultBytes(results),
			OriginalLines: len(results),
			ShownBytes:    countResultBytes(res.Results),
			ShownLines:    len(res.Results),
			Style:         "head",
		}
	} else {
		res.Results = results
	}

	return res, nil
}

// countResultBytes computes the byte length of search-result
// lines (path:line: content) for use in Truncation metadata.
func countResultBytes(results []SearchResult) int {
	total := 0
	for _, m := range results {
		total += len(m.Path) + 1 // path + ':'
		total += len(fmt.Sprintf("%d", m.LineNumber)) + 2 // line + ": "
		total += len(m.Content) + 1 // content + '\n'
	}
	return total
}

// searchFile scans a single file for lines matching the given regex.
func searchFile(path string, re *regexp.Regexp) ([]SearchResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	results := make([]SearchResult, 0)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			results = append(results, SearchResult{
				Path:       path,
				LineNumber: lineNum,
				Content:    line,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan file: %w", err)
	}
	return results, nil
}

// SearchFilesTool is the tool.Tool descriptor for SearchFiles.
var SearchFilesTool = tool.Tool{
	Name:        "search_files",
	Description: "Search files for lines matching a regex query. Returns matches with file path, line number, and matching line content. If the path is a directory, searches recursively. Hidden files and directories are skipped.\n\n" +
		"Output limits: capped at 1000 matches by default. Use the limit " +
		"parameter to control the cap. When truncated, the result includes a " +
		"hint to use a higher limit or a more specific regex.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The relative or absolute file or directory path to search.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "The regex pattern to search for.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of matches to return. Defaults to 1000.",
				"default":     frameworkDefaultSearchRows,
			},
		},
		"required": []string{"path", "query"},
	},
	DisplayHint: func(args map[string]any) any {
		return fmt.Sprintf("🔍 search_files(%s, %s)", toString(args["path"]), toString(args["query"]))
	},
	Format: tool.Format{
		Truncate: tool.TruncateConfig{
			MaxLines: frameworkDefaultSearchRows,
		},
		Style:        tool.StyleHead,
		RecoveryHint: "Output truncated. Use limit=2N to see more matches, or refine the regex.",
	},
}

// toInt safely extracts an integer from a JSON-decoded number (float64 or int)
// with a default value.
func toInt(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case float32:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case uint:
		return int(n)
	case string:
		// Attempt basic parse, fall back to default on error.
		var i int
		_, err := fmt.Sscanf(strings.TrimSpace(n), "%d", &i)
		if err != nil {
			return def
		}
		return i
	}
	return def
}

// toString safely extracts a string value from a JSON-decoded argument.
func toString(v any) string {
	s, _ := v.(string)
	return s
}
