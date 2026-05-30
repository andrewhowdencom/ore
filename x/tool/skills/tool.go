package skills

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/tool"
)

// Toolkit binds a Catalog to a tool.ToolFunc implementation that an LLM
// can invoke to load skill instructions on demand.
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

// ReadSkill returns the full SKILL.md content for the named skill.
// Parameters:
//   - name (string, required): the skill name.
func (t *Toolkit) ReadSkill(ctx context.Context, _ tool.Sandbox, args map[string]any) (any, error) {
	name := toString(args["name"])
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	return t.catalog.Read(ctx, name)
}

// SystemPromptFragment returns a prompt fragment function suitable for
// systemprompt.WithContextContentFunc. The returned function delegates to
// the underlying Catalog.SystemPromptFragment, producing a formatted listing
// of all discovered skills.
func (t *Toolkit) SystemPromptFragment() func(context.Context) string {
	return t.catalog.SystemPromptFragment
}

// SetDirective overrides the default behavioral directive used in the
// system prompt fragment. It delegates to the underlying Catalog and
// is safe for concurrent use.
func (t *Toolkit) SetDirective(directive string) {
	t.catalog.SetDirective(directive)
}

// Register adds the read_skill tool to the registry.
func (t *Toolkit) Register(registry tool.Registry) error {
	if err := registry.Register(
		ReadSkillTool.Name,
		ReadSkillTool.Description,
		ReadSkillTool.Schema,
		t.ReadSkill,
		tool.WithDisplay(ReadSkillDisplayHint),
	); err != nil {
		return fmt.Errorf("register read_skill: %w", err)
	}
	return nil
}

// ReadSkillDisplayHint returns a human-readable display for read_skill calls.
func ReadSkillDisplayHint(args map[string]any) any {
	return fmt.Sprintf("📖 read_skill(%s)", toString(args["name"]))
}

// ReadSkillTool is the provider.Tool descriptor for read_skill.
var ReadSkillTool = provider.Tool{
	Name:        "read_skill",
	Description: "Read the full SKILL.md content for a named skill. Use this to load the detailed instructions for a skill.",
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
	DisplayHint: ReadSkillDisplayHint,
}



// toString safely extracts a string value from a JSON-decoded argument.
func toString(v any) string {
	s, _ := v.(string)
	return s
}
