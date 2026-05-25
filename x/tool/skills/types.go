package skills

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// Discoverer abstracts where skills live. Implementations scan a source
// (filesystem, embedded FS, etc.) for SKILL.md files and expose metadata
// and full content on demand.
type Discoverer interface {
	// Discover returns metadata for all skills found in this source.
	Discover(ctx context.Context) ([]SkillMeta, error)
	// Read returns the full SKILL.md content for the skill with the given name.
	Read(ctx context.Context, name string) (string, error)
}

// SkillMeta holds the publicly disclosed metadata for a skill.
type SkillMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// discovererEntry pairs metadata with the discoverer that provided it.
type discovererEntry struct {
	meta        SkillMeta
	discoverer  Discoverer
}

// Catalog aggregates results from multiple Discoverers, caches metadata,
// handles deduplication (first-wins), and exposes List, Read, and Search.
type Catalog struct {
	mu          sync.RWMutex
	discoverers []Discoverer
	cache       map[string]discovererEntry
}

// NewCatalog creates a Catalog backed by the given discoverers.
func NewCatalog(discoverers ...Discoverer) *Catalog {
	return &Catalog{
		discoverers: discoverers,
		cache:       make(map[string]discovererEntry),
	}
}

// List returns metadata for all discovered skills, deterministically sorted
// by name. The cache is refreshed if empty.
func (c *Catalog) List(ctx context.Context) ([]SkillMeta, error) {
	c.mu.RLock()
	empty := len(c.cache) == 0
	c.mu.RUnlock()

	if empty {
		if err := c.refresh(ctx); err != nil {
			return nil, fmt.Errorf("failed to refresh catalog: %w", err)
		}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]SkillMeta, 0, len(c.cache))
	for _, entry := range c.cache {
		result = append(result, entry.meta)
	}

	// Deterministic sort by name.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// Read returns the full SKILL.md content for the skill with the given name.
// The cache is refreshed if the name is not found.
func (c *Catalog) Read(ctx context.Context, name string) (string, error) {
	c.mu.RLock()
	_, ok := c.cache[name]
	c.mu.RUnlock()

	if !ok {
		if err := c.refresh(ctx); err != nil {
			return "", fmt.Errorf("failed to refresh catalog: %w", err)
		}
	}

	c.mu.RLock()
	entry, ok := c.cache[name]
	c.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}

	content, err := entry.discoverer.Read(ctx, name)
	if err != nil {
		return "", fmt.Errorf("failed to read skill %q: %w", name, err)
	}

	return content, nil
}

// Search returns metadata for skills whose name or description contains the
// query as a case-insensitive substring.
func (c *Catalog) Search(ctx context.Context, query string) ([]SkillMeta, error) {
	all, err := c.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list skills: %w", err)
	}

	query = strings.ToLower(query)
	result := make([]SkillMeta, 0)
	for _, meta := range all {
		if strings.Contains(strings.ToLower(meta.Name), query) ||
			strings.Contains(strings.ToLower(meta.Description), query) {
			result = append(result, meta)
		}
	}
	return result, nil
}

// SystemPromptFragment returns a formatted prompt fragment listing all
// discovered skills. The resulting bullet list is deterministic because
// Catalog.List returns skills sorted by name. If no skills are discovered
// or an error occurs, it returns an empty string so the section is omitted
// from the prompt.
func (c *Catalog) SystemPromptFragment(ctx context.Context) string {
	meta, err := c.List(ctx)
	if err != nil || len(meta) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("You have access to the following specialized skills. Use read_skill(name=<skill>) to load detailed instructions when needed:\n\n")
	for i, m := range meta {
		b.WriteString("- ")
		b.WriteString(m.Name)
		b.WriteString(": ")
		b.WriteString(m.Description)
		if i < len(meta)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// refresh queries all discoverers, builds a name → entry map with
// first-wins deduplication, and replaces the cache under a write lock.
// Individual discoverer errors are logged and skipped rather than failing
// the entire refresh.
func (c *Catalog) refresh(ctx context.Context) error {
	newCache := make(map[string]discovererEntry)

	for _, d := range c.discoverers {
		metaList, err := d.Discover(ctx)
		if err != nil {
			slog.Warn("discoverer failed during catalog refresh, skipping", "error", err)
			continue
		}
		for _, meta := range metaList {
			if _, exists := newCache[meta.Name]; exists {
				slog.Warn("duplicate skill name detected during catalog refresh, skipping", "name", meta.Name)
				continue // first-wins
			}
			newCache[meta.Name] = discovererEntry{
				meta:       meta,
				discoverer: d,
			}
		}
	}

	c.mu.Lock()
	c.cache = newCache
	c.mu.Unlock()

	return nil
}
