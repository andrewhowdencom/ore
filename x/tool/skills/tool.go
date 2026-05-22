package skills

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/x/tool"
)

// Toolkit binds a Catalog to three tool.ToolFunc implementations that an LLM
// can invoke to discover and load skill instructions on demand.
type Toolkit struct {
	catalog *Catalog
}

// NewToolkit creates a Toolkit backed by a Catalog composed from the given
// discoverers.
func NewToolkit(discoverers ...Discoverer) *Toolkit {
	return &Toolkit{
		catalog: NewCatalog(discoverers...),
	}
}

// ListSkills lists all discovered skills by metadata (name + description).
// Parameters: none.
func (t *Toolkit) ListSkills(ctx context.Context, args map[string]any) (any, error) {
	return t.catalog.List(ctx)
}

// ReadSkill returns the full SKILL.md content for the named skill.
// Parameters:
//   - name (string, required): the skill name.
func (t *Toolkit) ReadSkill(ctx context.Context, args map[string]any) (any, error) {
	name := toString(args["name"])
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	return t.catalog.Read(ctx, name)
}

// SearchSkills searches skills by case-insensitive substring match on name
// or description.
// Parameters:
//   - query (string, required): the search query.
func (t *Toolkit) SearchSkills(ctx context.Context, args map[string]any) (any, error) {
	query := toString(args["query"])
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	return t.catalog.Search(ctx, query)
}

// Register adds all three skills tools to the registry.
func (t *Toolkit) Register(registry *tool.Registry) {
	registry.Register(
		ListSkillsTool.Name,
		ListSkillsTool.Description,
		ListSkillsTool.Schema,
		t.ListSkills,
	)
	registry.Register(
		ReadSkillTool.Name,
		ReadSkillTool.Description,
		ReadSkillTool.Schema,
		t.ReadSkill,
	)
	registry.Register(
		SearchSkillsTool.Name,
		SearchSkillsTool.Description,
		SearchSkillsTool.Schema,
		t.SearchSkills,
	)
}

// ListSkillsTool is the provider.Tool descriptor for list_skills.
var ListSkillsTool = provider.Tool{
	Name:        "list_skills",
	Description: "List all available skills. Returns an array of skill metadata (name and description) for every discovered SKILL.md file.",
	Schema: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
}

// ReadSkillTool is the provider.Tool descriptor for read_skill.
var ReadSkillTool = provider.Tool{
	Name:        "read_skill",
	Description: "Read the full SKILL.md content for a named skill. Use this after list_skills or search_skills to load the detailed instructions for a skill.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "The name of the skill to read.",
			},
		},
		"required": []string{"name"},
	},
}

// SearchSkillsTool is the provider.Tool descriptor for search_skills.
var SearchSkillsTool = provider.Tool{
	Name:        "search_skills",
	Description: "Search skills by name or description. Returns an array of skill metadata matching the case-insensitive query.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The case-insensitive substring to search for in skill names and descriptions.",
			},
		},
		"required": []string{"query"},
	},
}

// toString safely extracts a string value from a JSON-decoded argument.
func toString(v any) string {
	s, _ := v.(string)
	return s
}
