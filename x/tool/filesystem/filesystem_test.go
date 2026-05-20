package filesystem

import (
	"context"
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
