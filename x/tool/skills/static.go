package skills

import (
	"context"
	"fmt"
)

// Skill is the full record of a skill: name, description, and content.
// SkillMeta is the catalog-facing projection of a Skill — see Meta.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"-"`
}

// Meta returns the metadata projection of this skill, suitable for inclusion
// in a Catalog. Catalog-facing code uses SkillMeta; full content stays in
// Skill.Content and is returned by Discoverer.Read implementations.
func (s Skill) Meta() SkillMeta {
	return SkillMeta{Name: s.Name, Description: s.Description}
}

// StaticSource is a Discoverer backed by an in-memory slice of skills.
// Useful for tests, runtime-defined skills, and the BuiltInSkills registry.
//
// StaticSource does not deduplicate entries by name — it returns entries in
// the slice's natural order. Deduplication is the responsibility of the
// Catalog that aggregates this source with others.
type StaticSource []Skill

// Compile-time assertion that StaticSource satisfies Discoverer.
var _ Discoverer = StaticSource(nil)

// Discover projects each Skill to SkillMeta and returns them in input order.
func (s StaticSource) Discover(_ context.Context) ([]SkillMeta, error) {
	metas := make([]SkillMeta, len(s))
	for i, sk := range s {
		metas[i] = sk.Meta()
	}
	return metas, nil
}

// Read returns the full content of the named skill, or an error if no entry
// has a matching Name. Matching is a linear scan.
func (s StaticSource) Read(_ context.Context, name string) (string, error) {
	for _, sk := range s {
		if sk.Name == name {
			return sk.Content, nil
		}
	}
	return "", fmt.Errorf("skill %q not found", name)
}