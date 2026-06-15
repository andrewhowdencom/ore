package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
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
	r := result.(*ReadFileResult)
	assert.Equal(t, "1|line one\n2|line two\n3|line three\n", r.Content)
	assert.Nil(t, r.Truncation)
	assert.Empty(t, r.TempFilePath)
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
	r := result.(*ReadFileResult)
	assert.Equal(t, "2|line two\n3|line three\n", r.Content)
}

func TestReadFile_OffsetZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "offset": 0})
	require.NoError(t, err)
	r := result.(*ReadFileResult)
	assert.Equal(t, "1|line one\n2|line two\n", r.Content)
}

func TestReadFile_OffsetBeyondEOF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "offset": 10})
	require.NoError(t, err)
	r := result.(*ReadFileResult)
	assert.Equal(t, "", r.Content)
}

func TestReadFile_LimitZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two\nline three\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "limit": 0})
	require.NoError(t, err)
	r := result.(*ReadFileResult)
	assert.Equal(t, "1|line one\n2|line two\n3|line three\n", r.Content)
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
	r := result.(*ReadFileResult)
	assert.Equal(t, "", r.Content)
}

func TestReadFile_NoTrailingNewline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "no-nl.txt")
	require.NoError(t, os.WriteFile(p, []byte("line one\nline two"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p})
	require.NoError(t, err)
	r := result.(*ReadFileResult)
	assert.Equal(t, "1|line one\n2|line two\n", r.Content)
}

func TestReadFile_ByteCapTruncation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "big.txt")
	// Each line is ~100 bytes; 1000 lines = ~100 KB. The default
	// cap is 50 KB / 2000 lines, so the byte cap dominates.
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString(fmt.Sprintf("line %05d: this is a moderately long line of text for size to push us over the cap\n", i))
	}
	full := sb.String()
	require.NoError(t, os.WriteFile(p, []byte(full), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p})
	require.NoError(t, err)
	r := result.(*ReadFileResult)

	require.NotNil(t, r.Truncation, "Truncation should be non-nil for 200 KB output")
	assert.LessOrEqual(t, len(r.Content), 50_000)
	assert.NotEmpty(t, r.TempFilePath, "temp file path should be set on truncation")

	// Verify the temp file exists and contains the full content.
	t.Cleanup(func() { os.Remove(r.TempFilePath) })
	contents, err := os.ReadFile(r.TempFilePath)
	require.NoError(t, err)
	assert.Equal(t, len(full), len(contents), "temp file should hold the unmodified file content")
}

func TestReadFile_MarshalLLM_NoTruncation(t *testing.T) {
	t.Parallel()
	r := &ReadFileResult{Content: "1|hello\n2|world\n"}
	assert.Equal(t, "1|hello\n2|world\n", r.MarshalLLM())
}

func TestReadFile_MarshalLLM_Truncated(t *testing.T) {
	t.Parallel()
	r := &ReadFileResult{
		Content:      "1|first line\n",
		TempFilePath: "/tmp/ore-readfile-abc.txt",
		Truncation: &artifact.Truncation{
			OriginalBytes: 100_000,
			OriginalLines: 1000,
			ShownBytes:    20,
			ShownLines:    1,
			Style:         "head",
		},
	}
	got := r.MarshalLLM()
	assert.Contains(t, got, "1|first line\n")
	assert.Contains(t, got, "offset=2")
	assert.Contains(t, got, "/tmp/ore-readfile-abc.txt")
	assert.Contains(t, got, "lines shown of")
}

func TestReadFile_MarshalMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		result  ReadFileResult
		want    []string
		notWant []string
	}{
		{
			name:   "content wrapped in code fence",
			result: ReadFileResult{Content: "1|hello\n2|world\n"},
			want: []string{
				"```",
				"1|hello",
				"2|world",
				"```",
			},
			notWant: []string{
				"Output truncated",
			},
		},
		{
			name:   "empty content is a bare fence pair",
			result: ReadFileResult{Content: ""},
			want: []string{
				"```\n```",
			},
		},
		{
			name: "content without trailing newline gets one inside the fence",
			// The reader always emits a trailing '\n', so this case
			// is a safety net: it should still close the fence cleanly.
			result: ReadFileResult{Content: "1|hello"},
			want: []string{
				"```\n1|hello\n```",
			},
		},
		{
			name: "truncated result includes recovery hint with temp file path",
			result: ReadFileResult{
				Content:      "1|first line\n2|second line\n",
				TempFilePath: "/tmp/ore-readfile-abc.txt",
				Truncation: &artifact.Truncation{
					OriginalBytes: 100_000,
					OriginalLines: 1000,
					ShownBytes:    20,
					ShownLines:    2,
					Style:         "head",
				},
			},
			want: []string{
				"```",
				"1|first line",
				"2|second line",
				"```",
				"Output truncated",
				"2 of 1000 lines shown",
				"/tmp/ore-readfile-abc.txt",
				"offset=3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := tt.result.MarshalMarkdown()
			for _, s := range tt.want {
				if !strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() missing %q\ngot:\n%s", s, md)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() unexpectedly contains %q\ngot:\n%s", s, md)
				}
			}
		})
	}
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
	r := result.(*WriteFileResult)
	assert.Equal(t, fmt.Sprintf("wrote %d bytes to %q", len("hello world"), p), r.Message)
	assert.Equal(t, p, r.Path)
	assert.Equal(t, len("hello world"), r.Bytes)

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
	r := result.(*WriteFileResult)
	assert.Contains(t, r.Message, "nested.txt")

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "nested content", string(data))
}

func TestWriteFile_Overwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "exists.txt")
	require.NoError(t, os.WriteFile(p, []byte("existing"), 0o644))

	result, err := WriteFile(context.Background(), nil, map[string]any{
		"path":    p,
		"content": "new content",
	})
	require.NoError(t, err)
	r := result.(*WriteFileResult)
	assert.Contains(t, r.Message, "wrote")

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "new content", string(data))
}

func TestWriteFile_DirectoryAtPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "subdir")
	require.NoError(t, os.Mkdir(p, 0o755))

	_, err := WriteFile(context.Background(), nil, map[string]any{
		"path":    p,
		"content": "should fail",
	})
	require.Error(t, err) // os.WriteFile on a directory returns an error
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
	r := result.(*WriteFileResult)
	assert.Equal(t, fmt.Sprintf("wrote 0 bytes to %q", p), r.Message)
	assert.Equal(t, 0, r.Bytes)

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
	r := result.(*EditFileResult)
	assert.Equal(t, fmt.Sprintf("edited %q", p), r.Message)
	assert.Equal(t, p, r.Path)

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
	r := result.(*EditFileResult)
	assert.Equal(t, fmt.Sprintf("edited %q", p), r.Message)

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
	r := result.(*EditFileResult)
	assert.Equal(t, fmt.Sprintf("edited %q", p), r.Message)

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
	r := result.(*EditFileResult)
	assert.Equal(t, fmt.Sprintf("edited %q", p), r.Message)

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "XX ab ab\n", string(data))
}

func TestWriteFile_MarshalMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		result  WriteFileResult
		want    []string
		notWant []string
	}{
		{
			name: "ack wrapped in code fence",
			result: WriteFileResult{
				Path:    "/tmp/foo.go",
				Bytes:   42,
				Message: `wrote 42 bytes to "/tmp/foo.go"`,
			},
			want: []string{
				"```\nwrote 42 bytes to \"/tmp/foo.go\"\n```",
			},
			notWant: []string{
				`\"/tmp/foo.go\"`, // no JSON-escaped quotes
			},
		},
		{
			name: "empty message is a bare fence pair",
			result: WriteFileResult{
				Message: "",
			},
			want: []string{
				"```\n```",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := tt.result.MarshalMarkdown()
			for _, s := range tt.want {
				if !strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() missing %q\ngot:\n%s", s, md)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() unexpectedly contains %q\ngot:\n%s", s, md)
				}
			}
		})
	}
}

func TestEditFile_MarshalMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result EditFileResult
		want   []string
	}{
		{
			name: "ack wrapped in code fence",
			result: EditFileResult{
				Path:    "/tmp/foo.go",
				Message: `edited "/tmp/foo.go"`,
			},
			want: []string{
				"```\nedited \"/tmp/foo.go\"\n```",
			},
		},
		{
			name: "empty message is a bare fence pair",
			result: EditFileResult{
				Message: "",
			},
			want: []string{
				"```\n```",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := tt.result.MarshalMarkdown()
			for _, s := range tt.want {
				if !strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() missing %q\ngot:\n%s", s, md)
				}
			}
		})
	}
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
	r := result.(*ListDirectoryResult)
	assert.Equal(t, []string{"a.txt", "b.txt", "c_dir"}, r.Entries)
	assert.Nil(t, r.Truncation)
}

func TestListDirectory_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	result, err := ListDirectory(context.Background(), nil, map[string]any{"path": dir})
	require.NoError(t, err)
	r := result.(*ListDirectoryResult)
	assert.Equal(t, []string{}, r.Entries)
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
	r := result.(*ListDirectoryResult)
	assert.Equal(t, []string{"visible"}, r.Entries)
}

func TestListDirectory_RowCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Create 600 files; the default cap is 500.
	for i := 0; i < 600; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, fmt.Sprintf("file%04d.txt", i)),
			[]byte("x"), 0o644,
		))
	}

	result, err := ListDirectory(context.Background(), nil, map[string]any{"path": dir})
	require.NoError(t, err)
	r := result.(*ListDirectoryResult)

	assert.Len(t, r.Entries, 500)
	require.NotNil(t, r.Truncation, "Truncation should be non-nil at the row cap")
	assert.Equal(t, 600, r.Truncation.OriginalLines)
	assert.Equal(t, 500, r.Truncation.ShownLines)
}

func TestListDirectory_RowCap_WithLimitArg(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := 0; i < 100; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, fmt.Sprintf("file%04d.txt", i)),
			[]byte("x"), 0o644,
		))
	}

	// Caller asks for 10; cap applies.
	result, err := ListDirectory(context.Background(), nil, map[string]any{
		"path":  dir,
		"limit": 10.0,
	})
	require.NoError(t, err)
	r := result.(*ListDirectoryResult)

	assert.Len(t, r.Entries, 10)
	require.NotNil(t, r.Truncation)
}

func TestListDirectory_MarshalLLM_NoTruncation(t *testing.T) {
	t.Parallel()
	r := &ListDirectoryResult{Entries: []string{"a", "b", "c"}}
	assert.Equal(t, "a\nb\nc", r.MarshalLLM())
}

func TestListDirectory_MarshalLLM_Truncated(t *testing.T) {
	t.Parallel()
	r := &ListDirectoryResult{
		Entries: []string{"a", "b"},
		Truncation: &artifact.Truncation{
			OriginalBytes: 1000,
			OriginalLines: 100,
			ShownBytes:    20,
			ShownLines:    2,
			Style:         "head",
		},
	}
	got := r.MarshalLLM()
	assert.Contains(t, got, "a\nb")
	assert.Contains(t, got, "limit=2N")
	assert.Contains(t, got, "entries shown of")
}

func TestListDirectory_MarshalMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		result  ListDirectoryResult
		want    []string
		notWant []string
	}{
		{
			name:   "entries wrapped in code fence, one per line",
			result: ListDirectoryResult{Entries: []string{"a", "b", "c"}},
			want: []string{
				"```\na\nb\nc\n```",
			},
		},
		{
			name:   "empty entry list is a bare fence pair",
			result: ListDirectoryResult{Entries: nil},
			want: []string{
				"```\n```",
			},
		},
		{
			name: "truncated result includes recovery hint",
			result: ListDirectoryResult{
				Entries: []string{"a", "b"},
				Truncation: &artifact.Truncation{
					OriginalBytes: 1000,
					OriginalLines: 100,
					ShownBytes:    20,
					ShownLines:    2,
					Style:         "head",
				},
			},
			want: []string{
				"```\na\nb\n```",
				"Output truncated",
				"2 of 100 entries shown",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := tt.result.MarshalMarkdown()
			for _, s := range tt.want {
				if !strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() missing %q\ngot:\n%s", s, md)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() unexpectedly contains %q\ngot:\n%s", s, md)
				}
			}
		})
	}
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
	r := result.(*SearchFilesResult)

	require.Len(t, r.Results, 2)
	assert.Equal(t, 1, r.Results[0].LineNumber)
	assert.Equal(t, "hello world", r.Results[0].Content)
	assert.Equal(t, 3, r.Results[1].LineNumber)
	assert.Equal(t, "hello moon", r.Results[1].Content)
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
	r := result.(*SearchFilesResult)

	require.Len(t, r.Results, 2)
	assert.Equal(t, p1, r.Results[0].Path)
	assert.Equal(t, 1, r.Results[0].LineNumber)
	assert.Equal(t, "alpha", r.Results[0].Content)
	assert.Equal(t, p2, r.Results[1].Path)
	assert.Equal(t, 1, r.Results[1].LineNumber)
	assert.Equal(t, "gamma", r.Results[1].Content)
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
	r := result.(*SearchFilesResult)
	assert.Len(t, r.Results, 0)
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
	r := result.(*SearchFilesResult)
	require.Len(t, r.Results, 1)
	assert.Equal(t, visible, r.Results[0].Path)
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
	r := result.(*SearchFilesResult)
	require.Len(t, r.Results, 1)
	assert.Equal(t, visible, r.Results[0].Path)
}

func TestSearchFiles_RowCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "search.txt")
	// 1500 matching lines; default cap is 1000.
	var sb strings.Builder
	for i := 0; i < 1500; i++ {
		sb.WriteString(fmt.Sprintf("match line %d\n", i))
	}
	require.NoError(t, os.WriteFile(p, []byte(sb.String()), 0o644))

	result, err := SearchFiles(context.Background(), nil, map[string]any{
		"path":  p,
		"query": "match",
	})
	require.NoError(t, err)
	r := result.(*SearchFilesResult)

	assert.Len(t, r.Results, 1000)
	require.NotNil(t, r.Truncation)
	assert.Equal(t, 1500, r.Truncation.OriginalLines)
	assert.Equal(t, 1000, r.Truncation.ShownLines)
}

func TestSearchFiles_MarshalLLM_NoTruncation(t *testing.T) {
	t.Parallel()
	r := &SearchFilesResult{
		Results: []SearchResult{
			{Path: "/a/b.go", LineNumber: 10, Content: "foo"},
			{Path: "/c/d.go", LineNumber: 20, Content: "bar"},
		},
	}
	got := r.MarshalLLM()
	assert.Contains(t, got, "/a/b.go:10: foo")
	assert.Contains(t, got, "/c/d.go:20: bar")
}

func TestSearchFiles_MarshalLLM_Truncated(t *testing.T) {
	t.Parallel()
	r := &SearchFilesResult{
		Results: []SearchResult{{Path: "/a", LineNumber: 1, Content: "x"}},
		Truncation: &artifact.Truncation{
			OriginalBytes: 100_000,
			OriginalLines: 2000,
			ShownBytes:    50,
			ShownLines:    1,
			Style:         "head",
		},
	}
	got := r.MarshalLLM()
	assert.Contains(t, got, "limit=2N")
	assert.Contains(t, got, "matches shown of")
}

func TestSearchFiles_MarshalMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		result  SearchFilesResult
		want    []string
		notWant []string
	}{
		{
			name: "matches wrapped in code fence with path:line: framing",
			result: SearchFilesResult{
				Results: []SearchResult{
					{Path: "/a.go", LineNumber: 1, Content: "package main"},
					{Path: "/a.go", LineNumber: 5, Content: "func main() {}"},
				},
			},
			want: []string{
				"```\n/a.go:1: package main\n/a.go:5: func main() {}\n```",
			},
		},
		{
			name:   "empty result set is a bare fence pair",
			result: SearchFilesResult{Results: nil},
			want: []string{
				"```\n```",
			},
		},
		{
			name: "truncated result includes recovery hint",
			result: SearchFilesResult{
				Results: []SearchResult{{Path: "/a", LineNumber: 1, Content: "x"}},
				Truncation: &artifact.Truncation{
					OriginalBytes: 100_000,
					OriginalLines: 2000,
					ShownBytes:    50,
					ShownLines:    1,
					Style:         "head",
				},
			},
			want: []string{
				"```\n/a:1: x\n```",
				"Output truncated",
				"1 of 2000 matches shown",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := tt.result.MarshalMarkdown()
			for _, s := range tt.want {
				if !strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() missing %q\ngot:\n%s", s, md)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() unexpectedly contains %q\ngot:\n%s", s, md)
				}
			}
		})
	}
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

// rejectAbsSandbox is a FileSandbox that rejects absolute paths.
type rejectAbsSandbox struct {
	dir string
}

func (m *rejectAbsSandbox) Name() string { return "reject-abs" }
func (m *rejectAbsSandbox) ResolvePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths not allowed in sandbox %q", m.Name())
	}
	return filepath.Join(m.dir, path), nil
}
func (m *rejectAbsSandbox) WorkingDirectory() string { return m.dir }

var _ tool.FileSandbox = (*rejectAbsSandbox)(nil)

func TestReadFile_WithSandbox(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("hello"), 0o644))

	sb := &mockFileSandbox{dir: dir}
	result, err := ReadFile(context.Background(), sb, map[string]any{"path": "test.txt"})
	require.NoError(t, err)
	r := result.(*ReadFileResult)
	assert.Equal(t, "1|hello\n", r.Content)
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
	r := result.(*ReadFileResult)
	assert.Equal(t, "1|hello\n", r.Content)
}

func TestReadFile_SandboxRejectsAbsolutePath(t *testing.T) {
	t.Parallel()
	sb := &rejectAbsSandbox{dir: t.TempDir()}
	_, err := ReadFile(context.Background(), sb, map[string]any{"path": "/etc/passwd"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute paths not allowed")
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
	r := result.(*WriteFileResult)
	assert.Contains(t, r.Message, "new.txt")

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
	r := result.(*ReadFileResult)
	assert.Equal(t, "1|line1\n2|line2\n3|line3\n", r.Content)
}

func TestReadFile_ZeroLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0o644))

	result, err := ReadFile(context.Background(), nil, map[string]any{"path": p, "offset": 1, "limit": 0})
	require.NoError(t, err)
	r := result.(*ReadFileResult)
	assert.Equal(t, "1|line1\n2|line2\n3|line3\n", r.Content)
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
