package skills

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingDiscoverer records the (name, path) arguments passed to Read
// and returns a configurable result. Used by reference-path tests that
// need to inspect the forwarding behavior.
type recordingDiscoverer struct {
	meta  []SkillMeta
	onRead func(name, path string) (string, error)
}

func (r *recordingDiscoverer) Discover(_ context.Context) ([]SkillMeta, error) {
	return r.meta, nil
}

func (r *recordingDiscoverer) Read(_ context.Context, name string, path string) (string, error) {
	return r.onRead(name, path)
}

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

func TestToolkit_ReadSkill_Reference(t *testing.T) {
	t.Parallel()
	// Verify path is forwarded to the discoverer.
	var receivedName, receivedPath string
	d := &recordingDiscoverer{
		meta: []SkillMeta{{Name: "go", Description: "test"}},
		onRead: func(name, path string) (string, error) {
			receivedName = name
			receivedPath = path
			return "# Reference Body", nil
		},
	}
	tk := NewToolkit(d)
	result, err := tk.ReadSkill(context.Background(), nil, map[string]any{
		"name": "go",
		"path": "references/foo.md",
	})
	require.NoError(t, err)
	r := result.(*ReadSkillResult)
	assert.Equal(t, "go", receivedName)
	assert.Equal(t, "references/foo.md", receivedPath)
	assert.Equal(t, "# Reference Body", r.Content)
	assert.Nil(t, r.Truncation)
}

func TestToolkit_ReadSkill_ReferenceTruncated(t *testing.T) {
	t.Parallel()
	// Same truncation behavior applies to reference reads.
	big := strings.Repeat("b", 100_000)
	tk := NewToolkit(&mockDiscoverer{
		meta:  []SkillMeta{{Name: "big", Description: "big"}},
		reads: map[string]string{"big": big},
	})
	result, err := tk.ReadSkill(context.Background(), nil, map[string]any{
		"name": "big",
		"path": "references/big.md",
	})
	require.NoError(t, err)
	r := result.(*ReadSkillResult)
	require.NotNil(t, r.Truncation, "Truncation should be non-nil for 100 KB reference")
	assert.LessOrEqual(t, len(r.Content), 50_000)
	assert.NotEmpty(t, r.TempFilePath)
	t.Cleanup(func() { os.Remove(r.TempFilePath) })
	contents, err := os.ReadFile(r.TempFilePath)
	require.NoError(t, err)
	assert.Equal(t, big, string(contents))
}

func TestToolkit_ReadSkill_ReferenceNotFound(t *testing.T) {
	t.Parallel()
	tk := NewToolkit(&mockDiscoverer{
		meta: []SkillMeta{{Name: "x", Description: "y"}},
		reads: map[string]string{"x": "content"},
	})
	_, err := tk.ReadSkill(context.Background(), nil, map[string]any{
		"name": "x",
		"path": "references/missing.md",
	})
	// The mock doesn't differentiate; it always returns the same content
	// regardless of path. To exercise not-found, use a discoverer that
	// errors on a specific path.
	tk2 := NewToolkit(&recordingDiscoverer{
		meta: []SkillMeta{{Name: "x", Description: "y"}},
		onRead: func(name, path string) (string, error) {
			return "", fmt.Errorf("reference %q not found", path)
		},
	})
	_, err = tk2.ReadSkill(context.Background(), nil, map[string]any{
		"name": "x",
		"path": "references/missing.md",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference")
	assert.Contains(t, err.Error(), "references/missing.md")
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
