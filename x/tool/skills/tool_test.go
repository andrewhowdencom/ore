package skills

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolkit_ReadSkill(t *testing.T) {
	t.Parallel()
	tk := NewToolkit(&mockDiscoverer{
		meta: []SkillMeta{
			{Name: "test", Description: "a test skill"},
		},
		reads: map[string]string{"test": "full content"},
	})
	result, err := tk.ReadSkill(context.Background(), nil, map[string]any{"name": "test"})
	require.NoError(t, err)
	r := result.(*ReadSkillResult)
	assert.Equal(t, "full content", r.Content)
	assert.Nil(t, r.Truncation)
	assert.Empty(t, r.TempFilePath)
}

func TestToolkit_ReadSkill_Truncated(t *testing.T) {
	t.Parallel()
	// Build a skill content larger than the default 50 KB cap.
	big := strings.Repeat("a", 100_000)
	tk := NewToolkit(&mockDiscoverer{
		meta: []SkillMeta{
			{Name: "big", Description: "big skill"},
		},
		reads: map[string]string{"big": big},
	})
	result, err := tk.ReadSkill(context.Background(), nil, map[string]any{"name": "big"})
	require.NoError(t, err)
	r := result.(*ReadSkillResult)
	require.NotNil(t, r.Truncation, "Truncation should be non-nil for 100 KB output")
	assert.LessOrEqual(t, len(r.Content), 50_000)
	assert.NotEmpty(t, r.TempFilePath)
	t.Cleanup(func() { os.Remove(r.TempFilePath) })
	contents, err := os.ReadFile(r.TempFilePath)
	require.NoError(t, err)
	assert.Equal(t, big, string(contents))
}

func TestReadSkillResult_MarshalLLM_NoTruncation(t *testing.T) {
	t.Parallel()
	r := &ReadSkillResult{Content: "hello"}
	assert.Equal(t, "hello", r.MarshalLLM())
}

func TestReadSkillResult_MarshalLLM_Truncated(t *testing.T) {
	t.Parallel()
	r := &ReadSkillResult{
		Content:      "head",
		TempFilePath: "/tmp/ore-skill-abc.md",
		Truncation: &artifact.Truncation{
			OriginalBytes: 100_000,
			OriginalLines: 2000,
			ShownBytes:    4,
			ShownLines:    1,
			Style:         "head",
		},
	}
	got := r.MarshalLLM()
	assert.Contains(t, got, "head")
	assert.Contains(t, got, "/tmp/ore-skill-abc.md")
	assert.Contains(t, got, "lines shown of")
}

func TestToolkit_ReadSkill_MissingName(t *testing.T) {
	t.Parallel()
	tk := NewToolkit()
	_, err := tk.ReadSkill(context.Background(), nil, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestToolkit_ReadSkill_EmptyName(t *testing.T) {
	t.Parallel()
	tk := NewToolkit()
	_, err := tk.ReadSkill(context.Background(), nil, map[string]any{"name": ""})
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
	_, err := tk.ReadSkill(context.Background(), nil, map[string]any{"name": "missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
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

	expected := "When your task matches a skill description below, call read_skill(name=<skill>) to load its detailed instructions before proceeding.\n\n- alpha: first skill\n- beta: second skill"
	assert.Equal(t, expected, fragment)
}

func TestToolkit_SystemPromptFragment_ErrorFallback(t *testing.T) {
	t.Parallel()
	tk := NewToolkit(&failingDiscoverer{})
	fn := tk.SystemPromptFragment()
	fragment := fn(context.Background())
	assert.Empty(t, fragment)
}

func TestToolkit_SetDirective(t *testing.T) {
	t.Parallel()
	tk := NewToolkit(&mockDiscoverer{
		meta: []SkillMeta{
			{Name: "alpha", Description: "first skill"},
		},
	})
	tk.SetDirective("Custom directive text.")
	fn := tk.SystemPromptFragment()
	fragment := fn(context.Background())

	expected := "Custom directive text.\n\n- alpha: first skill"
	assert.Equal(t, expected, fragment)
}

func TestToolkit_SetDirective_PartialFailure(t *testing.T) {
	t.Parallel()
	good := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "good-skill", Description: "from good discoverer"},
		},
	}
	tk := NewToolkit(good, &failingDiscoverer{})
	tk.SetDirective("Custom directive text.")
	fn := tk.SystemPromptFragment()
	fragment := fn(context.Background())

	expected := "Custom directive text.\n\n- good-skill: from good discoverer"
	assert.Equal(t, expected, fragment)
}
