package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// FSDiscoverer scans a directory tree for SKILL.md files.
type FSDiscoverer struct {
	Root  string
	paths map[string]string
	mu    sync.RWMutex
}

// NewFSDiscoverer creates a filesystem-backed Discoverer rooted at root.
func NewFSDiscoverer(root string) *FSDiscoverer {
	return &FSDiscoverer{
		Root:  root,
		paths: make(map[string]string),
	}
}

// Discover walks Root recursively and returns metadata for every valid
// SKILL.md found. Malformed files are skipped with a logged warning.
func (d *FSDiscoverer) Discover(ctx context.Context) ([]SkillMeta, error) {
	paths := make(map[string]string)
	var result []SkillMeta

	err := filepath.WalkDir(d.Root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			slog.Warn("skipping unreadable path during skill discovery", "path", path, "error", err)
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Base(path) != "SKILL.md" {
			return nil
		}

		meta, err := parseSkillFile(path, os.ReadFile)
		if err != nil {
			slog.Warn("skipping malformed SKILL.md", "path", path, "error", err)
			return nil
		}

		if _, exists := paths[meta.Name]; exists {
			slog.Warn("duplicate skill name in filesystem discoverer, skipping", "name", meta.Name, "path", path)
			return nil
		}

		paths[meta.Name] = path
		result = append(result, meta)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory %q: %w", d.Root, err)
	}

	d.mu.Lock()
	d.paths = paths
	d.mu.Unlock()

	return result, nil
}

// Read returns the full SKILL.md content for the named skill.
func (d *FSDiscoverer) Read(ctx context.Context, name string) (string, error) {
	d.mu.RLock()
	path, ok := d.paths[name]
	d.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("skill %q not found in filesystem discoverer", name)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read skill file %q: %w", path, err)
	}
	return string(data), nil
}

// readFileFunc abstracts over os.ReadFile and embed.FS.ReadFile.
type readFileFunc func(path string) ([]byte, error)

// parseSkill parses the bytes of a SKILL.md file. It validates YAML
// frontmatter and returns a Skill with Content set to the full file
// bytes (frontmatter + body), matching what FSDiscoverer.Read and
// EmbeddedDiscoverer.Read already return to the LLM.
func parseSkill(data []byte) (Skill, error) {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	parts := strings.SplitN(content, "\n---\n", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "---") {
		return Skill{}, fmt.Errorf("missing or invalid YAML frontmatter")
	}

	var frontmatter struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	yamlPart := strings.TrimPrefix(parts[0], "---")
	yamlPart = strings.TrimSpace(yamlPart)
	if err := yaml.Unmarshal([]byte(yamlPart), &frontmatter); err != nil {
		return Skill{}, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}

	if strings.TrimSpace(frontmatter.Name) == "" {
		return Skill{}, fmt.Errorf("missing required field: name")
	}
	if strings.TrimSpace(frontmatter.Description) == "" {
		return Skill{}, fmt.Errorf("missing required field: description")
	}

	return Skill{
		Name:        frontmatter.Name,
		Description: frontmatter.Description,
		Content:     string(data),
	}, nil
}

// parseSkillFile reads a SKILL.md file via the supplied read function,
// validates YAML frontmatter, and returns the extracted metadata as a
// SkillMeta. It is a thin wrapper around parseSkill that preserves the
// readFileFunc indirection used by FSDiscoverer and EmbeddedDiscoverer.
func parseSkillFile(path string, readFn readFileFunc) (SkillMeta, error) {
	data, err := readFn(path)
	if err != nil {
		return SkillMeta{}, fmt.Errorf("failed to read file: %w", err)
	}
	skill, err := parseSkill(data)
	if err != nil {
		return SkillMeta{}, err
	}
	return skill.Meta(), nil
}
