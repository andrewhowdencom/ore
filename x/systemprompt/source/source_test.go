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

func TestAgentReferenceIndex(t *testing.T) {
	const buildAgent = `---
description: Execute a structured development plan from .plans/<plan-name>.md
argument-hint: "<plan-name>"
---

## Identity
You are a build agent.
`
	const planAgent = `---
description: Create a structured software development plan
argument-hint: "<plan-name> [requirements...]"
---

## Identity
You are a planning agent.
`
	const ideationAgent = `---
description: "Conversational ideation partner — explore ideas, surface tradeoffs"
---

## Identity
You are an ideation agent.
`
	const malformedAgent = `# No frontmatter here
Just a body.
`
	const noFieldsAgent = `---
random-key: value
---

## Identity
Body
`

	tests := []struct {
		name     string
		active   string
		setup    func(t *testing.T) string
		expected string
	}{
		{
			name:   "missing directory returns empty string",
			active: "build",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "does-not-exist")
			},
			expected: "",
		},
		{
			name:   "empty directory returns empty string",
			active: "build",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			expected: "",
		},
		{
			name:   "only active agent present returns empty string",
			active: "build",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), []byte(buildAgent), 0644))
				return dir
			},
			expected: "",
		},
		{
			name:   "multiple agents, active excluded",
			active: "build",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), []byte(buildAgent), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "plan.md"), []byte(planAgent), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "ideation.md"), []byte(ideationAgent), 0644))
				return dir
			},
			expected: "## Other Available Agents\n\n" +
				"- **ideation**: Conversational ideation partner — explore ideas, surface tradeoffs.\n" +
				"- **plan**: Create a structured software development plan. `<plan-name> [requirements...]`",
		},
		{
			name:   "missing frontmatter fields render with filename only",
			active: "build",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), []byte(buildAgent), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "blank.md"), []byte(noFieldsAgent), 0644))
				return dir
			},
			expected: "## Other Available Agents\n\n- **blank**",
		},
		{
			name:   "malformed agent file is still listed with filename",
			active: "build",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), []byte(buildAgent), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.md"), []byte(malformedAgent), 0644))
				return dir
			},
			expected: "## Other Available Agents\n\n- **broken**",
		},
		{
			name:   "non-md files are ignored",
			active: "build",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), []byte(buildAgent), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "plan.txt"), []byte("not markdown"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "ideation.md.bak"), []byte("backup"), 0644))
				return dir
			},
			expected: "",
		},
		{
			name:   "description with surrounding quotes is stripped",
			active: "build",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), []byte(buildAgent), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "ideation.md"), []byte(ideationAgent), 0644))
				return dir
			},
			expected: "## Other Available Agents\n\n" +
				"- **ideation**: Conversational ideation partner — explore ideas, surface tradeoffs.",
		},
		{
			name:   "empty activeName excludes nothing",
			active: "",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "build.md"), []byte(buildAgent), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "plan.md"), []byte(planAgent), 0644))
				return dir
			},
			expected: "## Other Available Agents\n\n" +
				"- **build**: Execute a structured development plan from .plans/<plan-name>.md. `<plan-name>`\n" +
				"- **plan**: Create a structured software development plan. `<plan-name> [requirements...]`",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setup(t)
			assert.Equal(t, tt.expected, AgentReferenceIndex(dir, tt.active)())
		})
	}
}

func TestAgentReferenceIndex_DynamicUpdate(t *testing.T) {
	dir := t.TempDir()
	buildPath := filepath.Join(dir, "build.md")
	planPath := filepath.Join(dir, "plan.md")
	require.NoError(t, os.WriteFile(buildPath, []byte(`---
description: Build agent
---

body
`), 0644))

	fn := AgentReferenceIndex(dir, "build")
	assert.Equal(t, "", fn()) // build excluded, plan absent

	require.NoError(t, os.WriteFile(planPath, []byte(`---
description: Plan agent
---

body
`), 0644))
	assert.Equal(t, "## Other Available Agents\n\n- **plan**: Plan agent.", fn())
}

func TestParseAgentFrontmatter(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		wantName          string
		wantDescription   string
		wantArgumentHint  string
	}{
		{
			name: "full frontmatter with description and argument-hint",
			input: `---
description: Build an agent
argument-hint: "<name>"
---

## Identity
body
`,
			wantName:         "",
			wantDescription:  "Build an agent",
			wantArgumentHint: "<name>",
		},
		{
			name: "frontmatter with description only",
			input: `---
description: Build an agent
---

body
`,
			wantName:         "",
			wantDescription:  "Build an agent",
			wantArgumentHint: "",
		},
		{
			name: "quoted description is stripped",
			input: `---
description: "Quoted description"
---
`,
			wantName:         "",
			wantDescription:  "Quoted description",
			wantArgumentHint: "",
		},
		{
			name: "single-quoted description is stripped",
			input: `---
description: 'single quoted'
---
`,
			wantName:         "",
			wantDescription:  "single quoted",
			wantArgumentHint: "",
		},
		{
			name:             "no frontmatter delimiters returns zero values",
			input:            "no frontmatter\njust body",
			wantName:         "",
			wantDescription:  "",
			wantArgumentHint: "",
		},
		{
			name: "unclosed frontmatter yields whatever lines were parseable",
			input: `---
description: dangling
`,
			wantName:         "",
			wantDescription:  "dangling",
			wantArgumentHint: "",
		},
		{
			name:             "empty content returns zero values",
			input:            "",
			wantName:         "",
			wantDescription:  "",
			wantArgumentHint: "",
		},
		{
			name: "extra keys are ignored",
			input: `---
name: ignored
description: real description
author: anonymous
---
`,
			wantName:         "",
			wantDescription:  "real description",
			wantArgumentHint: "",
		},
		{
			name: "lines without colons are skipped",
			input: `---
description: real
this line has no colon
argument-hint: "<x>"
---
`,
			wantName:         "",
			wantDescription:  "real",
			wantArgumentHint: "<x>",
		},
		{
			name: "CRLF line endings are normalized",
			input: "---\r\ndescription: crlf\r\n---\r\n",
			wantName:         "",
			wantDescription:  "crlf",
			wantArgumentHint: "",
		},
		{
			name: "leading horizontal whitespace is tolerated",
			input: "  ---\ndescription: indented\n---\n",
			wantName:         "",
			wantDescription:  "indented",
			wantArgumentHint: "",
		},
		{
			name: "empty description value is preserved",
			input: `---
description:
argument-hint: "<x>"
---
`,
			wantName:         "",
			wantDescription:  "",
			wantArgumentHint: "<x>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, description, argumentHint := parseAgentFrontmatter(tt.input)
			assert.Equal(t, tt.wantName, name, "name")
			assert.Equal(t, tt.wantDescription, description, "description")
			assert.Equal(t, tt.wantArgumentHint, argumentHint, "argumentHint")
		})
	}
}
