package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two\nline three\n"), 0o644))

	result, err := ReadFile(context.Background(), map[string]any{"path": p})
	require.NoError(t, err)
	assert.Equal(t, "1|line one\n2|line two\n3|line three\n", result)
}

func TestReadFile_OffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two\nline three\nline four\n"), 0o644))

	result, err := ReadFile(context.Background(), map[string]any{
		"path":   p,
		"offset": 2.0,
		"limit":  2.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "2|line two\n3|line three\n", result)
}

func TestReadFile_MissingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "missing.txt")

	_, err := ReadFile(context.Background(), map[string]any{"path": p})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat path")
}

func TestReadFile_Directory(t *testing.T) {
	dir := t.TempDir()

	_, err := ReadFile(context.Background(), map[string]any{"path": dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a directory")
}

func TestReadFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(p, []byte{}, 0o644))

	result, err := ReadFile(context.Background(), map[string]any{"path": p})
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestWriteFile_NewFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "new.txt")

	result, err := WriteFile(context.Background(), map[string]any{
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
	dir := t.TempDir()
	p := filepath.Join(dir, "subdir", "nested.txt")

	result, err := WriteFile(context.Background(), map[string]any{
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
	dir := t.TempDir()
	p := filepath.Join(dir, "exists.txt")
	require.NoError(t, os.WriteFile(p, []byte("existing"), 0o644))

	_, err := WriteFile(context.Background(), map[string]any{
		"path":    p,
		"content": "new content",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestWriteFile_DirectoryExists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "subdir")
	require.NoError(t, os.Mkdir(p, 0o755))

	_, err := WriteFile(context.Background(), map[string]any{
		"path":    p,
		"content": "should fail",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestWriteFile_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.txt")

	result, err := WriteFile(context.Background(), map[string]any{
		"path":    p,
		"content": "",
	})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("wrote 0 bytes to %q", p), result)

	info, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, int64(0), info.Size())
}

func TestToInt_Float64(t *testing.T) {
	assert.Equal(t, 42, toInt(42.0, 0))
}

func TestToInt_Int(t *testing.T) {
	assert.Equal(t, 42, toInt(42, 0))
}

func TestToInt_String(t *testing.T) {
	assert.Equal(t, 42, toInt("42", 0))
}

func TestToInt_Default(t *testing.T) {
	assert.Equal(t, 7, toInt(nil, 7))
	assert.Equal(t, 7, toInt("not-a-number", 7))
}
