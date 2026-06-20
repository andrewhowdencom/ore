package source

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// FileResolver is a content source that holds a mutable file path. Use
// NewFileResolver to construct one, SetPath to change the target file
// after the resolver has already been registered with a system prompt
// (or any other consumer of systemprompt.Resolver), and Resolve to read
// the current file's contents.
//
// The motivating use case is a system prompt that needs to reflect
// exactly one active content file at a time: register the resolver
// once via WithContentFunc(r.Resolve), then call SetPath when the
// active file should change. The next Transform will read from the
// new path. No new WithContentFunc call is required, which avoids the
// "stacking" pattern that produces contradictory content when
// multiple agent definitions are concatenated into a single RoleSystem
// turn.
//
// FileResolver is safe for concurrent use; Resolve and SetPath may be
// called from different goroutines.
type FileResolver struct {
	mu   sync.RWMutex
	path string
}

// NewFileResolver returns a FileResolver that initially reads from path.
// The path can be changed at any time via SetPath; the new path takes
// effect on the next Resolve call.
func NewFileResolver(path string) *FileResolver {
	return &FileResolver{path: path}
}

// SetPath updates the file that subsequent Resolve calls will read.
// It is safe to call concurrently with Resolve.
func (r *FileResolver) SetPath(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.path = path
}

// Path returns the path most recently passed to SetPath or NewFileResolver.
func (r *FileResolver) Path() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.path
}

// Resolve reads the current file and returns its contents. If the path
// is empty or the file cannot be read, Resolve returns an empty string.
// *FileResolver satisfies systemprompt.Resolver via this method.
func (r *FileResolver) Resolve() string {
	r.mu.RLock()
	path := r.path
	r.mu.RUnlock()
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
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
