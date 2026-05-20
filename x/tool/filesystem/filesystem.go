package filesystem

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/x/tool"
)

// Compile-time type checks.
var (
	_ tool.ToolFunc = ReadFile
	_ tool.ToolFunc = WriteFile
	_ tool.ToolFunc = EditFile
	_ tool.ToolFunc = ListDirectory
	_ tool.ToolFunc = SearchFiles
)

// ReadFile reads a file and returns its contents with line-number prefixes.
// Parameters:
//   - path (string, required): relative or absolute file path.
//   - offset (number, optional, default 1): 1-based starting line.
//   - limit  (number, optional, default 0): maximum lines to return (0 = no limit).
func ReadFile(ctx context.Context, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

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

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var result strings.Builder

	start := offset - 1
	if start < 0 {
		start = 0
	}
	if start >= len(lines) {
		return "", nil
	}

	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	for i := start; i < end; i++ {
		// Preserve original line content (including possible trailing spaces) but
		// don't add extra prefix to empty final line produced by Split on trailing newline.
		if i == len(lines)-1 && lines[i] == "" {
			continue
		}
		result.WriteString(fmt.Sprintf("%d|%s\n", i+1, lines[i]))
	}

	return result.String(), nil
}

// ReadFileTool is the provider.Tool descriptor for ReadFile.
var ReadFileTool = provider.Tool{
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
}

// WriteFile creates a new file with the given content.
// It fails if the path already exists (file or directory), forcing the
// agent to use edit_file for modifications.
// Parameters:
//   - path    (string, required): relative or absolute file path.
//   - content (string, required): file contents to write.
func WriteFile(ctx context.Context, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	content := toString(args["content"])

	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("path %q already exists", path)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create parent directories: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("wrote %d bytes to %q", len(content), path), nil
}

// WriteFileTool is the provider.Tool descriptor for WriteFile.
var WriteFileTool = provider.Tool{
	Name:        "write_file",
	Description: "Create a new file with the specified content. Fails if the path already exists, forcing the use of edit_file for modifications.",
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
}

// EditFile performs an exact-match search-and-replace on an existing file.
// It replaces the first occurrence of old_string with new_string.
// Parameters:
//   - path       (string, required): relative or absolute file path.
//   - old_string (string, required): exact text to search for.
//   - new_string (string, required): replacement text.
func EditFile(ctx context.Context, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

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

// EditFileTool is the provider.Tool descriptor for EditFile.
var EditFileTool = provider.Tool{
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
}

// ListDirectory returns a shallow listing of non-hidden entries in a directory.
// Parameters:
//   - path (string, required): relative or absolute directory path.
func ListDirectory(ctx context.Context, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

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

// ListDirectoryTool is the provider.Tool descriptor for ListDirectory.
var ListDirectoryTool = provider.Tool{
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
func SearchFiles(ctx context.Context, args map[string]any) (any, error) {
	path := toString(args["path"])
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
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

// SearchFilesTool is the provider.Tool descriptor for SearchFiles.
var SearchFilesTool = provider.Tool{
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
