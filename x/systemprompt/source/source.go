package source

import (
	"os"
	"path/filepath"
	"strings"
)

// File returns a content function that reads the given file path on every call.
// If the file does not exist or cannot be read, it returns an empty string.
func File(path string) func() string {
	return func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// Agent returns a content function that reads a single agent definition file
// from the given directory. The file is resolved as "<dir>/<name>.md" and
// re-read on every call (mirroring File). If the file does not exist, or
// the directory cannot be read, the returned function yields an empty string.
//
// Use this in preference to stacking multiple File("<dir>/<agent>.md") calls
// when assembling a system prompt from agent definitions. Loading more than
// one agent body into a single RoleSystem turn produces contradictory
// Identity sections; see the "Multi-Identity Stacking" section of
// x/systemprompt's package documentation for details.
//
// The raw file content is returned without any frontmatter processing —
// the file as a whole defines the identity. Callers that need only the
// post-frontmatter body should parse it themselves.
func Agent(dir, name string) func() string {
	return func() string {
		return File(filepath.Join(dir, name+".md"))()
	}
}

// AgentReferenceIndex returns a content function that renders a compact
// markdown index of every "<dir>/*.md" agent file except the active one.
// Each entry is parsed from its YAML frontmatter for description and
// argument-hint; the agent's name is taken from the filename.
//
// The rendered output is suitable for inclusion alongside source.Agent
// in a system prompt: the active agent's full body is in context, and
// the other agents are summarised by name + description so the LLM
// knows they exist without being given conflicting instructions.
//
// If the directory is unreadable, contains no other agents, or every
// other agent fails to parse, the returned function yields an empty
// string. This lets consumers include it unconditionally without
// producing empty-section noise.
//
// Reads every "<dir>/*.md" file on each invocation; the function does
// not cache. For directories with many agent files this may become a
// bottleneck — add a memoization layer in a higher-level consumer if so.
func AgentReferenceIndex(dir, activeName string) func() string {
	return func() string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ""
		}

		var lines []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			fileName := entry.Name()
			if !strings.HasSuffix(fileName, ".md") {
				continue
			}
			agentName := strings.TrimSuffix(fileName, ".md")
			if agentName == activeName {
				continue
			}

			content, err := os.ReadFile(filepath.Join(dir, fileName))
			if err != nil {
				continue
			}
			_, description, argumentHint := parseAgentFrontmatter(string(content))
			lines = append(lines, formatAgentIndexEntry(agentName, description, argumentHint))
		}

		if len(lines) == 0 {
			return ""
		}
		return "## Other Available Agents\n\n" + strings.Join(lines, "\n")
	}
}

// formatAgentIndexEntry renders a single bullet for AgentReferenceIndex.
// The shape depends on which fields are populated:
//   - description + argument-hint: "- **name**: description. `hint`"
//   - description only:            "- **name**: description."
//   - argument-hint only:          "- **name**: `hint`"
//   - neither:                     "- **name**"
func formatAgentIndexEntry(name, description, argumentHint string) string {
	switch {
	case description != "" && argumentHint != "":
		return "- **" + name + "**: " + description + ". `" + argumentHint + "`"
	case description != "":
		return "- **" + name + "**: " + description + "."
	case argumentHint != "":
		return "- **" + name + "**: `" + argumentHint + "`"
	default:
		return "- **" + name + "**"
	}
}

// parseAgentFrontmatter extracts the description and argument-hint fields
// from an agent file's YAML frontmatter. The leading and trailing "---"
// delimiters are required; content without them yields zero values.
//
// The name field is intentionally not extracted — the agent's filename
// (minus ".md") is the canonical identity, and consumer code substitutes
// the filename regardless of any frontmatter "name:" key.
//
// The parser tolerates malformed input by returning zero values for any
// field it could not extract; it never panics. Single-line scalar values
// are the only supported shape; multi-line YAML values, lists, or anchors
// are not parsed and will fall back to zero for that field.
func parseAgentFrontmatter(content string) (name, description, argumentHint string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimLeft(content, " \t")
	if !strings.HasPrefix(content, "---\n") && content != "---" {
		return "", "", ""
	}

	rest := content
	if strings.HasPrefix(rest, "---\n") {
		rest = rest[4:]
	} else {
		rest = ""
	}

	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "---" {
			break
		}
		idx := strings.Index(trimmed, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		value := strings.TrimSpace(trimmed[idx+1:])
		// Strip a single layer of surrounding matching quotes.
		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		switch key {
		case "description":
			description = value
		case "argument-hint":
			argumentHint = value
			// "name" is intentionally ignored — see the doc comment above.
		}
	}
	return name, description, argumentHint
}

// agentsMDFiles lists the instruction file names to discover, in priority
// order, at each directory level during the parent-directory walk.
var agentsMDFiles = []string{"AGENTS.md", "CLAUDE.md"}

// AgentsMD returns a content function that walks parent directories from
// startDir toward the filesystem root, discovering AGENTS.md and CLAUDE.md
// files at each level. All found files are concatenated in discovery order
// (nearest first), separated by "\n\n". If no files are found, it returns an
// empty string.
func AgentsMD(startDir string) func() string {
	return func() string {
		var contents []string
		dir := startDir

		for {
			for _, name := range agentsMDFiles {
				p := filepath.Join(dir, name)
				b, err := os.ReadFile(p)
				if err != nil {
					continue
				}
				contents = append(contents, string(b))
			}

			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}

		return strings.Join(contents, "\n\n")
	}
}

// Harness returns a content function that describes the agent harness.
// If name is empty, it returns an empty string.
func Harness(name string) func() string {
	return func() string {
		if name == "" {
			return ""
		}
		return "You are the " + name + " agent."
	}
}

// Model returns a content function that describes the model being used.
// If name is empty, it returns an empty string.
func Model(name string) func() string {
	return func() string {
		if name == "" {
			return ""
		}
		return "You are running on model " + name + "."
	}
}

// Provider returns a content function that describes the provider backend.
// If name is empty, it returns an empty string.
func Provider(name string) func() string {
	return func() string {
		if name == "" {
			return ""
		}
		return "Provider backend: " + name
	}
}
