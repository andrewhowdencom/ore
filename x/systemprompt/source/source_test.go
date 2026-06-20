package source

import (
	"os"
	"path/filepath"
	"testing"

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

func TestAgent(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) (dir, agentName string)
		expected string
	}{
		{
			name: "existing agent file returns content",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), []byte("# build agent body"), 0644))
				return dir, "build"
			},
			expected: "# build agent body",
		},
		{
			name: "missing agent file returns empty string",
			setup: func(t *testing.T) (string, string) {
				return t.TempDir(), "nope"
			},
			expected: "",
		},
		{
			name: "missing directory returns empty string",
			setup: func(t *testing.T) (string, string) {
				return filepath.Join(t.TempDir(), "does-not-exist"), "build"
			},
			expected: "",
		},
		{
			name: "empty agent file returns empty string",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), nil, 0644))
				return dir, "build"
			},
			expected: "",
		},
		{
			name: "filename with colons is resolved directly",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "review:docs:dev.md"), []byte("review body"), 0644))
				return dir, "review:docs:dev"
			},
			expected: "review body",
		},
		{
			name: "raw content is preserved including frontmatter",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				body := "---\ndescription: build agent\n---\n\n## Identity\nbody"
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), []byte(body), 0644))
				return dir, "build"
			},
			expected: "---\ndescription: build agent\n---\n\n## Identity\nbody",
		},
		{
			name: "non-md sibling is not picked up",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.txt"), []byte("wrong file"), 0644))
				return dir, "build"
			},
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, name := tt.setup(t)
			assert.Equal(t, tt.expected, Agent(dir, name)())
		})
	}
}

func TestAgent_DynamicUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "build.md")
	require.NoError(t, os.WriteFile(path, []byte("first"), 0644))

	fn := Agent(dir, "build")
	assert.Equal(t, "first", fn())

	require.NoError(t, os.WriteFile(path, []byte("second"), 0644))
	assert.Equal(t, "second", fn())
}
