package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolkit_ListSkills(t *testing.T) {
	t.Parallel()
	tk := NewToolkit(&mockDiscoverer{
		meta: []SkillMeta{
			{Name: "alpha", Description: "first"},
			{Name: "beta", Description: "second"},
		},
	})
	result, err := tk.ListSkills(context.Background(), nil)
	require.NoError(t, err)
	meta := result.([]SkillMeta)
	assert.Len(t, meta, 2)
	assert.Equal(t, "alpha", meta[0].Name)
	assert.Equal(t, "beta", meta[1].Name)
}

func TestToolkit_ReadSkill(t *testing.T) {
	t.Parallel()
	tk := NewToolkit(&mockDiscoverer{
		meta: []SkillMeta{
			{Name: "test", Description: "a test skill"},
		},
		reads: map[string]string{"test": "full content"},
	})
	result, err := tk.ReadSkill(context.Background(), map[string]any{"name": "test"})
	require.NoError(t, err)
	assert.Equal(t, "full content", result)
}

func TestToolkit_ReadSkill_MissingName(t *testing.T) {
	t.Parallel()
	tk := NewToolkit()
	_, err := tk.ReadSkill(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestToolkit_ReadSkill_EmptyName(t *testing.T) {
	t.Parallel()
	tk := NewToolkit()
	_, err := tk.ReadSkill(context.Background(), map[string]any{"name": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestToolkit_ReadSkill_Nonexistent(t *testing.T) {
	t.Parallel()
	tk := NewToolkit(&mockDiscoverer{
		meta: []SkillMeta{
			{Name: "existing", Description: "only one"},
		},
	})
	_, err := tk.ReadSkill(context.Background(), map[string]any{"name": "missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestToolkit_SearchSkills(t *testing.T) {
	t.Parallel()
	tk := NewToolkit(&mockDiscoverer{
		meta: []SkillMeta{
			{Name: "conduit", Description: "Implements a conduit."},
			{Name: "go", Description: "Go guidelines."},
		},
	})
	result, err := tk.SearchSkills(context.Background(), map[string]any{"query": "go"})
	require.NoError(t, err)
	meta := result.([]SkillMeta)
	assert.Len(t, meta, 1)
	assert.Equal(t, "go", meta[0].Name)
}

func TestToolkit_SearchSkills_MissingQuery(t *testing.T) {
	t.Parallel()
	tk := NewToolkit()
	_, err := tk.SearchSkills(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

func TestToolkit_SearchSkills_EmptyQuery(t *testing.T) {
	t.Parallel()
	tk := NewToolkit()
	_, err := tk.SearchSkills(context.Background(), map[string]any{"query": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

func TestToolkit_SystemPromptFragment(t *testing.T) {
	t.Parallel()
	tk := NewToolkit(&mockDiscoverer{
		meta: []SkillMeta{
			{Name: "alpha", Description: "first skill"},
			{Name: "beta", Description: "second skill"},
		},
	})
	fn := tk.SystemPromptFragment()
	fragment := fn(context.Background())

	expected := "You have access to the following specialized skills. Use read_skill(name=<skill>) to load detailed instructions when needed:\n\n- alpha: first skill\n- beta: second skill"
	assert.Equal(t, expected, fragment)
}
