package source

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/x/systemprompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFile(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		expected string
	}{
		{
			name: "existing file returns content",
			setup: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "prompt.txt")
				require.NoError(t, os.WriteFile(p, []byte("hello world"), 0644))
				return p
			},
			expected: "hello world",
		},
		{
			name: "missing file returns empty string",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "missing.txt")
			},
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)
			assert.Equal(t, tt.expected, File(path)())
		})
	}
}

func TestAgentsMD(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		expected string
	}{
		{
			name: "no files found returns empty string",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			expected: "",
		},
		{
			name: "single AGENTS.md in start dir",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents content"), 0644))
				return dir
			},
			expected: "agents content",
		},
		{
			name: "single AGENTS.md in parent dir",
			setup: func(t *testing.T) string {
				parent := t.TempDir()
				child := filepath.Join(parent, "child")
				require.NoError(t, os.MkdirAll(child, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(parent, "AGENTS.md"), []byte("parent agents"), 0644))
				return child
			},
			expected: "parent agents",
		},
		{
			name: "AGENTS.md and CLAUDE.md in same dir",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents content"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude content"), 0644))
				return dir
			},
			expected: "agents content\n\nclaude content",
		},
		{
			name: "multiple files in parent dirs concatenated nearest first",
			setup: func(t *testing.T) string {
				grandparent := t.TempDir()
				parent := filepath.Join(grandparent, "parent")
				child := filepath.Join(parent, "child")
				require.NoError(t, os.MkdirAll(child, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(child, "AGENTS.md"), []byte("child agents"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(parent, "CLAUDE.md"), []byte("parent claude"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(grandparent, "AGENTS.md"), []byte("grandparent agents"), 0644))
				return child
			},
			expected: "child agents\n\nparent claude\n\ngrandparent agents",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setup(t)
			assert.Equal(t, tt.expected, AgentsMD(dir)())
		})
	}
}

func TestAgentsMD_DynamicUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	require.NoError(t, os.WriteFile(path, []byte("first"), 0644))

	fn := AgentsMD(dir)
	assert.Equal(t, "first", fn())

	require.NoError(t, os.WriteFile(path, []byte("second"), 0644))
	assert.Equal(t, "second", fn())
}

func TestHarness(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "non-empty returns formatted string", input: "ideation", expected: "You are the ideation agent."},
		{name: "empty returns empty string", input: "", expected: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, Harness(tt.input)())
		})
	}
}

func TestModel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "non-empty returns formatted string", input: "gpt-4o", expected: "You are running on model gpt-4o."},
		{name: "empty returns empty string", input: "", expected: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, Model(tt.input)())
		})
	}
}

func TestProvider(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "non-empty returns formatted string", input: "openai", expected: "Provider backend: openai"},
		{name: "empty returns empty string", input: "", expected: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, Provider(tt.input)())
		})
	}
}

func TestFileResolver_SatisfiesResolver(t *testing.T) {
	// Compile-time guarantee: a *FileResolver can be used wherever a
	// systemprompt.Resolver is expected. If the interface changes,
	// this assignment fails to build.
	var _ systemprompt.Resolver = (*FileResolver)(nil)
}

func TestFileResolver(t *testing.T) {
	t.Run("Resolve returns file content for existing path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "content.md")
		require.NoError(t, os.WriteFile(path, []byte("hello world"), 0644))

		r := NewFileResolver(path)
		assert.Equal(t, "hello world", r.Resolve())
	})

	t.Run("Resolve returns empty string for missing file", func(t *testing.T) {
		r := NewFileResolver(filepath.Join(t.TempDir(), "missing.md"))
		assert.Equal(t, "", r.Resolve())
	})

	t.Run("Resolve returns empty string for empty path", func(t *testing.T) {
		r := NewFileResolver("")
		assert.Equal(t, "", r.Resolve())
	})

	t.Run("Resolve reflects dynamic file updates", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "content.md")
		require.NoError(t, os.WriteFile(path, []byte("first"), 0644))

		r := NewFileResolver(path)
		assert.Equal(t, "first", r.Resolve())

		require.NoError(t, os.WriteFile(path, []byte("second"), 0644))
		assert.Equal(t, "second", r.Resolve())
	})

	t.Run("SetPath changes the file Resolve reads", func(t *testing.T) {
		dir := t.TempDir()
		pathA := filepath.Join(dir, "a.md")
		pathB := filepath.Join(dir, "b.md")
		require.NoError(t, os.WriteFile(pathA, []byte("from A"), 0644))
		require.NoError(t, os.WriteFile(pathB, []byte("from B"), 0644))

		r := NewFileResolver(pathA)
		assert.Equal(t, "from A", r.Resolve())

		r.SetPath(pathB)
		assert.Equal(t, "from B", r.Resolve())
	})

	t.Run("SetPath then SetPath to empty returns empty string", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "content.md")
		require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))

		r := NewFileResolver(path)
		assert.Equal(t, "hello", r.Resolve())

		r.SetPath("")
		assert.Equal(t, "", r.Resolve())
	})

	t.Run("Path returns the most recently set path", func(t *testing.T) {
		r := NewFileResolver("/initial")
		assert.Equal(t, "/initial", r.Path())

		r.SetPath("/next")
		assert.Equal(t, "/next", r.Path())

		r.SetPath("")
		assert.Equal(t, "", r.Path())
	})
}

func TestFileResolver_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.md")
	pathB := filepath.Join(dir, "b.md")
	require.NoError(t, os.WriteFile(pathA, []byte("A"), 0644))
	require.NoError(t, os.WriteFile(pathB, []byte("B"), 0644))

	r := NewFileResolver(pathA)

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer goroutine: alternates SetPath between A and B.
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				r.SetPath(pathA)
			} else {
				r.SetPath(pathB)
			}
		}
	}()

	// Reader goroutine: Resolve must never panic and must always return
	// either "A" or "B".
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			got := r.Resolve()
			assert.True(t, got == "A" || got == "B", "unexpected Resolve result: %q", got)
		}
	}()

	wg.Wait()
}
