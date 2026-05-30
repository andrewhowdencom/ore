package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFile_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two\nline three\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p})
	require.NoError(t, err)
	assert.Equal(t, "1|line one\n2|line two\n3|line three\n", result)
}

func TestReadFile_OffsetAndLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two\nline three\nline four\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{
		"path":   p,
		"offset": 2.0,
		"limit":  2.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "2|line two\n3|line three\n", result)
}

func TestReadFile_OffsetZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "offset": 0})
	require.NoError(t, err)
	assert.Equal(t, "1|line one\n2|line two\n", result)
}

func TestReadFile_OffsetBeyondEOF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "offset": 10})
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestReadFile_LimitZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two\nline three\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "limit": 0})
	require.NoError(t, err)
	assert.Equal(t, "1|line one\n2|line two\n3|line three\n", result)
}

func TestReadFile_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := ReadFile(context.Background(), nil, map[string]any{"path": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

func TestReadFile_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "missing.txt")

	_, err := ReadFile(context.Background(), nil, map[string]any{"path": p})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat path")
}

func TestReadFile_Directory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := ReadFile(context.Background(), nil, map[string]any{"path": dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a directory")
}

func TestReadFile_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(p, []byte{}, 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p})
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestReadFile_NoTrailingNewline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "no-nl.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p})
	require.NoError(t, err)
	assert.Equal(t, "1|line one\n2|line two\n", result)
}

func TestWriteFile_NewFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "new.txt")

	result, err := WriteFile(context.Background(), nil, map[string]any{
		"path":    p,
		"content": "hello world",
	})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("wrote %d bytes to %q", len("hello world"), p), result)

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
}

func TestWriteFile_NestedPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "subdir", "nested.txt")

	result, err := WriteFile(context.Background(), nil, map[string]any{
		"path":    p,
		"content": "nested content",
	})
	require.NoError(t, err)
	assert.Contains(t, result.(string), "nested.txt")

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "nested content", string(data))
}

func TestWriteFile_AlreadyExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "exists.txt")
	require.NoError(t, os.WriteFile(p, []byte("existing"), 0o644))

	_, err := WriteFile(context.Background(), nil, map[string]any{
		"path":    p,
		"content": "new content",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestWriteFile_DirectoryExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "subdir")
	require.NoError(t, os.Mkdir(p, 0o755))

	_, err := WriteFile(context.Background(), nil, map[string]any{
		"path":    p,
		"content": "should fail",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestWriteFile_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := WriteFile(context.Background(), nil, map[string]any{
		"path":    "",
		"content": "content",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

func TestWriteFile_EmptyContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.txt")

	result, err := WriteFile(context.Background(), nil, map[string]any{
		"path":    p,
		"content": "",
	})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("wrote 0 bytes to %q", p), result)

	info, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, int64(0), info.Size())
}

func TestEditFile_SingleLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	require.NoError(t, os.WriteFile(p, []byte("hello world\n"), 0o644))

	result, err := EditFile(context.Background(), nil, map[string]any{
		"path":       p,
		"old_string": "hello",
		"new_string": "goodbye",
	})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("edited %q", p), result)

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "goodbye world\n", string(data))
}

func TestEditFile_MultiLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two\nline three\n"), 0o644))

	result, err := EditFile(context.Background(), nil, map[string]any{
		"path":       p,
		"old_string": "line two\nline three",
		"new_string": "replaced two\nreplaced three",
	})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("edited %q", p), result)

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "line one\nreplaced two\nreplaced three\n", string(data))
}

func TestEditFile_EmptyOldString(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	require.NoError(t, os.WriteFile(p, []byte("content\n"), 0o644))

	_, err := EditFile(context.Background(), nil, map[string]any{
		"path":       p,
		"old_string": "",
		"new_string": "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "old_string cannot be empty")
}

func TestEditFile_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	require.NoError(t, os.WriteFile(p, []byte("content\n"), 0o644))

	_, err := EditFile(context.Background(), nil, map[string]any{
		"path":       p,
		"old_string": "missing",
		"new_string": "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "old_string not found")
}

func TestEditFile_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "missing.txt")

	_, err := EditFile(context.Background(), nil, map[string]any{
		"path":       p,
		"old_string": "x",
		"new_string": "y",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestEditFile_EmptyNewString(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	require.NoError(t, os.WriteFile(p, []byte("hello world\n"), 0o644))

	result, err := EditFile(context.Background(), nil, map[string]any{
		"path":       p,
		"old_string": "hello",
		"new_string": "",
	})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("edited %q", p), result)

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, " world\n", string(data))
}

func TestEditFile_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := EditFile(context.Background(), nil, map[string]any{
		"path":       "",
		"old_string": "x",
		"new_string": "y",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

func TestEditFile_FirstOccurrenceOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "edit.txt")
	require.NoError(t, os.WriteFile(p, []byte("ab ab ab\n"), 0o644))

	result, err := EditFile(context.Background(), nil, map[string]any{
		"path":       p,
		"old_string": "ab",
		"new_string": "XX",
	})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("edited %q", p), result)

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "XX ab ab\n", string(data))
}

func TestListDirectory_MixedEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "c_dir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("hidden"), 0o644))

	result, err := ListDirectory(context.Background(), nil, map[string]any{"path": dir})
	require.NoError(t, err)
	assert.Equal(t, []string{"a.txt", "b.txt", "c_dir"}, result)
}

func TestListDirectory_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	result, err := ListDirectory(context.Background(), nil, map[string]any{"path": dir})
	require.NoError(t, err)
	assert.Equal(t, []string{}, result)
}

func TestListDirectory_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "missing")

	_, err := ListDirectory(context.Background(), nil, map[string]any{"path": p})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat path")
}

func TestListDirectory_FileAsPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(p, []byte("content"), 0o644))

	_, err := ListDirectory(context.Background(), nil, map[string]any{"path": p})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a directory")
}

func TestListDirectory_HiddenSubdirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "visible"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".hidden"), 0o755))

	result, err := ListDirectory(context.Background(), nil, map[string]any{"path": dir})
	require.NoError(t, err)
	assert.Equal(t, []string{"visible"}, result)
}

func TestSearchFiles_SingleFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "search.txt")
	require.NoError(t, os.WriteFile(p, []byte("hello world\ngoodbye world\nhello moon\n"), 0o644))

	result, err := SearchFiles(context.Background(), nil, map[string]any{
		"path":  p,
		"query": "hello",
	})
	require.NoError(t, err)

	results := result.([]SearchResult)
	require.Len(t, results, 2)
	assert.Equal(t, 1, results[0].LineNumber)
	assert.Equal(t, "hello world", results[0].Content)
	assert.Equal(t, 3, results[1].LineNumber)
	assert.Equal(t, "hello moon", results[1].Content)
}

func TestSearchFiles_DirectoryRecursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	require.NoError(t, os.Mkdir(sub, 0o755))
	p1 := filepath.Join(dir, "a.txt")
	p2 := filepath.Join(sub, "b.txt")
	require.NoError(t, os.WriteFile(p1, []byte("alpha\n"), 0o644))
	require.NoError(t, os.WriteFile(p2, []byte("gamma\n"), 0o644))

	result, err := SearchFiles(context.Background(), nil, map[string]any{
		"path":  dir,
		"query": "a",
	})
	require.NoError(t, err)

	results := result.([]SearchResult)
	require.Len(t, results, 2)
	assert.Equal(t, p1, results[0].Path)
	assert.Equal(t, 1, results[0].LineNumber)
	assert.Equal(t, "alpha", results[0].Content)
	assert.Equal(t, p2, results[1].Path)
	assert.Equal(t, 1, results[1].LineNumber)
	assert.Equal(t, "gamma", results[1].Content)
}

func TestSearchFiles_NoMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "search.txt")
	require.NoError(t, os.WriteFile(p, []byte("content\n"), 0o644))

	result, err := SearchFiles(context.Background(), nil, map[string]any{
		"path":  p,
		"query": "nomatch",
	})
	require.NoError(t, err)

	results := result.([]SearchResult)
	assert.Len(t, results, 0)
}

func TestSearchFiles_InvalidRegex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "search.txt")
	require.NoError(t, os.WriteFile(p, []byte("content\n"), 0o644))

	_, err := SearchFiles(context.Background(), nil, map[string]any{
		"path":  p,
		"query": "[invalid",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex")
}

func TestSearchFiles_SkipsHidden(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	visible := filepath.Join(dir, "visible.txt")
	hidden := filepath.Join(dir, ".hidden.txt")
	require.NoError(t, os.WriteFile(visible, []byte("visible content\n"), 0o644))
	require.NoError(t, os.WriteFile(hidden, []byte("hidden content\n"), 0o644))

	result, err := SearchFiles(context.Background(), nil, map[string]any{
		"path":  dir,
		"query": "content",
	})
	require.NoError(t, err)

	results := result.([]SearchResult)
	require.Len(t, results, 1)
	assert.Equal(t, visible, results[0].Path)
}

func TestSearchFiles_EmptyQuery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "search.txt")
	require.NoError(t, os.WriteFile(p, []byte("content\n"), 0o644))

	_, err := SearchFiles(context.Background(), nil, map[string]any{
		"path":  p,
		"query": "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

func TestSearchFiles_HiddenSubdirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	visible := filepath.Join(dir, "visible.txt")
	require.NoError(t, os.WriteFile(visible, []byte("match\n"), 0o644))
	hiddenDir := filepath.Join(dir, ".hidden_dir")
	require.NoError(t, os.Mkdir(hiddenDir, 0o755))
	hiddenFile := filepath.Join(hiddenDir, "hidden.txt")
	require.NoError(t, os.WriteFile(hiddenFile, []byte("match\n"), 0o644))

	result, err := SearchFiles(context.Background(), nil, map[string]any{
		"path":  dir,
		"query": "match",
	})
	require.NoError(t, err)

	results := result.([]SearchResult)
	require.Len(t, results, 1)
	assert.Equal(t, visible, results[0].Path)
}

func TestToInt_Float64(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 42, toInt(42.0, 0))
}

func TestToInt_Int(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 42, toInt(42, 0))
}

func TestToInt_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 42, toInt("42", 0))
}

func TestToInt_Default(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 7, toInt(nil, 7))
	assert.Equal(t, 7, toInt("not-a-number", 7))
}

func TestToString_NonString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", toString(42))
	assert.Equal(t, "", toString(nil))
	assert.Equal(t, "", toString(true))
}

type mockFileSandbox struct {
	dir string
}

func (m *mockFileSandbox) Name() string { return "mock" }
func (m *mockFileSandbox) ResolvePath(path string) (string, error) {
	return filepath.Join(m.dir, path), nil
}
func (m *mockFileSandbox) WorkingDirectory() string { return m.dir }

var _ tool.FileSandbox = (*mockFileSandbox)(nil)

type errorFileSandbox struct {
	dir string
}

func (m *errorFileSandbox) Name() string { return "error" }
func (m *errorFileSandbox) ResolvePath(path string) (string, error) {
	return "", fmt.Errorf("sandbox rejects path %q", path)
}
func (m *errorFileSandbox) WorkingDirectory() string { return m.dir }

var _ tool.FileSandbox = (*errorFileSandbox)(nil)

func TestReadFile_WithSandbox(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("hello"), 0o644))

	sb := &mockFileSandbox{dir: dir}
	result, err := ReadFile(context.Background(), sb, map[string]any{"path": "test.txt"})
	require.NoError(t, err)
	assert.Equal(t, "1|hello\n", result)
}

func TestReadFile_AbsolutePathInSandbox(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "etc", "passwd")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte("hello"), 0o644))

	sb := &mockFileSandbox{dir: dir}
	result, err := ReadFile(context.Background(), sb, map[string]any{"path": "/etc/passwd"})
	require.NoError(t, err)
	assert.Equal(t, "1|hello\n", result)
}

func TestWriteFile_WithSandbox(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sb := &mockFileSandbox{dir: dir}

	result, err := WriteFile(context.Background(), sb, map[string]any{
		"path":    "new.txt",
		"content": "hello world",
	})
	require.NoError(t, err)
	assert.Contains(t, result.(string), "new.txt")

	data, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
}

func TestReadFile_ResolvePathError(t *testing.T) {
	t.Parallel()
	sb := &errorFileSandbox{dir: t.TempDir()}
	_, err := ReadFile(context.Background(), sb, map[string]any{"path": "test.txt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox rejects path")
}

func TestReadFile_NegativeOffset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "offset": -5})
	require.NoError(t, err)
	assert.Equal(t, "1|line1\n2|line2\n3|line3\n", result)
}

func TestReadFile_ZeroLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "offset": 1, "limit": 0})
	require.NoError(t, err)
	assert.Equal(t, "1|line1\n2|line2\n3|line3\n", result)
}

func TestReadFile_BinaryRejection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "binary.bin")
	require.NoError(t, os.WriteFile(p, []byte("\xFF"), 0o644))

	_, err := ReadFile(context.Background(), nil, map[string]any{"path": p})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot read binary file")
	assert.Contains(t, err.Error(), p)
}

func TestReadFile_TotalCharacterCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "large.txt")

	var content strings.Builder
	for i := 0; i < 2000; i++ {
		content.WriteString(strings.Repeat("x", 60))
		content.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(p, []byte(content.String()), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p})
	require.NoError(t, err)
	resultStr := result.(string)
	require.LessOrEqual(t, len(resultStr), 100_000, "result should be capped at 100k chars")
	require.True(t, strings.HasSuffix(resultStr, "\n"), "truncation should occur at line boundary")
}

func TestReadFile_StreamingTruncationWithOffset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "large.txt")

	var content strings.Builder
	for i := 0; i < 2000; i++ {
		content.WriteString(strings.Repeat("x", 60))
		content.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(p, []byte(content.String()), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "offset": 500})
	require.NoError(t, err)
	resultStr := result.(string)
	require.True(t, strings.HasPrefix(resultStr, "500|"), "result should start at offset line 500")
	require.LessOrEqual(t, len(resultStr), 100_000, "result should still respect the 100k cap")
}

func TestReadFile_CapWithLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "large.txt")

	var content strings.Builder
	for i := 0; i < 2000; i++ {
		content.WriteString(strings.Repeat("x", 60))
		content.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(p, []byte(content.String()), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "offset": 1, "limit": 2000})
	require.NoError(t, err)
	resultStr := result.(string)
	require.LessOrEqual(t, len(resultStr), 100_000, "cap should win over limit")
}
