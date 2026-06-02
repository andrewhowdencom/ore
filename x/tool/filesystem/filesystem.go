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

	"github.com/andrewhowdencom/ore/tool"
)

// Compile-time type checks.
var (
	_ tool.ToolFunc = ReadFile
	_ tool.ToolFunc = WriteFile
	_ tool.ToolFunc = EditFile
	_ tool.ToolFunc = ListDirectory
	_ tool.ToolFunc = SearchFiles
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

// ReadFile reads a file and returns its contents with line-number prefixes.
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
	const maxChars = 100_000

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
		if result.Len()+len(formatted) > maxChars {
			break
		}

		result.WriteString(formatted)
		linesEmitted++

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("failed to read file: %w", readErr)
		}
	}

	return result.String(), nil
}

// ReadFileTool is the tool.Tool descriptor for ReadFile.
var ReadFileTool = tool.Tool{
	Name:        "read_file",
	Description: "Read the contents of a file. Returns the file contents with line-number prefixes. Optionally specify an offset (1-based starting line) and limit (maximum number of lines to return).",
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
}

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

	return fmt.Sprintf("wrote %d bytes to %q", len(content), path), nil
}

// WriteFileTool is the tool.Tool descriptor for WriteFile.
var WriteFileTool = tool.Tool{
	Name:        "write_file",
	Description: "Create or overwrite a file with the specified content.",
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

	return fmt.Sprintf("edited %q", path), nil
}

// EditFileTool is the tool.Tool descriptor for EditFile.
var EditFileTool = tool.Tool{
	Name:        "edit_file",
	Description: "Edit an existing file by replacing the first exact occurrence of old_string with new_string. Fails if old_string is empty or not found.",
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

// ListDirectory returns a shallow listing of non-hidden entries in a directory.
// Parameters:
//   - path (string, required): relative or absolute directory path.
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

	result := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		result = append(result, name)
	}

	return result, nil
}

// ListDirectoryTool is the tool.Tool descriptor for ListDirectory.
var ListDirectoryTool = tool.Tool{
	Name:        "list_directory",
	Description: "List the immediate non-hidden entries in a directory. Returns entry names. Hidden entries (names starting with '.') are excluded.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The relative or absolute path to the directory to list.",
			},
		},
		"required": []string{"path"},
	},
	DisplayHint: func(args map[string]any) any {
		return fmt.Sprintf("📁 list_directory(%s)", toString(args["path"]))
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

// SearchFiles searches files for lines matching a regex query.
// Parameters:
//   - path  (string, required): file or directory path to search.
//   - query (string, required): regex pattern to match.
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

	return results, nil
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
	Description: "Search files for lines matching a regex query. Returns matches with file path, line number, and matching line content. If the path is a directory, searches recursively. Hidden files and directories are skipped.",
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
		},
		"required": []string{"path", "query"},
	},
	DisplayHint: func(args map[string]any) any {
		return fmt.Sprintf("🔍 search_files(%s, %s)", toString(args["path"]), toString(args["query"]))
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
