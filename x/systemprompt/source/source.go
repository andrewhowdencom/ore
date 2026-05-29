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
