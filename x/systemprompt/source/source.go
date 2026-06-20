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
