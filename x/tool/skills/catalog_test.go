package skills

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDiscoverer is an in-memory Discoverer for testing.
type mockDiscoverer struct {
	meta  []SkillMeta
	reads map[string]string
}

func (m *mockDiscoverer) Discover(ctx context.Context) ([]SkillMeta, error) {
	return m.meta, nil
}

func (m *mockDiscoverer) Read(ctx context.Context, name string) (string, error) {
	content, ok := m.reads[name]
	if !ok {
		return "", fmt.Errorf("skill %q not found in mock", name)
	}
	return content, nil
}

// failingDiscoverer always returns an error.
type failingDiscoverer struct{}

func (f *failingDiscoverer) Discover(ctx context.Context) ([]SkillMeta, error) {
	return nil, fmt.Errorf("discovery failed")
}

func (f *failingDiscoverer) Read(ctx context.Context, name string) (string, error) {
	return "", fmt.Errorf("read failed")
}

func TestCatalog_SingleDiscoverer(t *testing.T) {
	t.Parallel()
	d := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "beta", Description: "second skill"},
			{Name: "alpha", Description: "first skill"},
		},
	}
	c := NewCatalog(d)
	meta, err := c.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, meta, 2)
	assert.Equal(t, "alpha", meta[0].Name)
	assert.Equal(t, "beta", meta[1].Name)
}

func TestCatalog_MultipleDiscoverers(t *testing.T) {
	t.Parallel()
	d1 := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "skill-a", Description: "from discoverer one"},
		},
	}
	d2 := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "skill-b", Description: "from discoverer two"},
		},
	}
	c := NewCatalog(d1, d2)
	meta, err := c.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, meta, 2)
	assert.Equal(t, "skill-a", meta[0].Name)
	assert.Equal(t, "skill-b", meta[1].Name)
}

func TestCatalog_DuplicateNamesFirstWins(t *testing.T) {
	t.Parallel()
	d1 := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "overlap", Description: "first wins"},
		},
		reads: map[string]string{"overlap": "first content"},
	}
	d2 := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "overlap", Description: "second should lose"},
		},
		reads: map[string]string{"overlap": "second content"},
	}
	c := NewCatalog(d1, d2)
	meta, err := c.List(context.Background())
	require.NoError(t, err)
	require.Len(t, meta, 1)
	assert.Equal(t, "first wins", meta[0].Description)

	// Read should delegate to the first discoverer.
	content, err := c.Read(context.Background(), "overlap")
	require.NoError(t, err)
	assert.Equal(t, "first content", content)
}

func TestCatalog_SearchMatches(t *testing.T) {
	t.Parallel()
	d := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "conduit", Description: "Implements a new ore I/O conduit package."},
			{Name: "go", Description: "Guidelines for Go development."},
		},
	}
	c := NewCatalog(d)

	byName, err := c.Search(context.Background(), "conduit")
	require.NoError(t, err)
	assert.Len(t, byName, 1)
	assert.Equal(t, "conduit", byName[0].Name)

	byDesc, err := c.Search(context.Background(), "guidelines")
	require.NoError(t, err)
	assert.Len(t, byDesc, 1)
	assert.Equal(t, "go", byDesc[0].Name)

	caseInsensitive, err := c.Search(context.Background(), "GO")
	require.NoError(t, err)
	assert.Len(t, caseInsensitive, 1)
	assert.Equal(t, "go", caseInsensitive[0].Name)
}

func TestCatalog_SearchNoMatches(t *testing.T) {
	t.Parallel()
	d := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "conduit", Description: "Implements a new ore I/O conduit package."},
		},
	}
	c := NewCatalog(d)
	result, err := c.Search(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestCatalog_ReadAfterRefresh(t *testing.T) {
	t.Parallel()
	d := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "test-skill", Description: "a test skill"},
		},
		reads: map[string]string{"test-skill": "full skill content here"},
	}
	c := NewCatalog(d)
	content, err := c.Read(context.Background(), "test-skill")
	require.NoError(t, err)
	assert.Equal(t, "full skill content here", content)
}

func TestCatalog_ReadNonexistent(t *testing.T) {
	t.Parallel()
	d := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "existing", Description: "only one"},
		},
	}
	c := NewCatalog(d)
	_, err := c.Read(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCatalog_RefreshSkipsFailingDiscoverer(t *testing.T) {
	t.Parallel()
	good := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "good-skill", Description: "from good discoverer"},
		},
	}
	bad := &failingDiscoverer{}
	c := NewCatalog(good, bad)
	meta, err := c.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, meta, 1)
	assert.Equal(t, "good-skill", meta[0].Name)
}

func TestCatalog_SystemPromptFragment(t *testing.T) {
	t.Parallel()
	d := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "beta", Description: "second skill"},
			{Name: "alpha", Description: "first skill"},
		},
	}
	c := NewCatalog(d)
	fragment := c.SystemPromptFragment(context.Background())

	expected := "When your task matches a skill description below, call read_skill(name=<skill>) to load its detailed instructions before proceeding.\n\n- alpha: first skill\n- beta: second skill"
	assert.Equal(t, expected, fragment)
}

func TestCatalog_SystemPromptFragment_Empty(t *testing.T) {
	t.Parallel()
	c := NewCatalog()
	fragment := c.SystemPromptFragment(context.Background())
	assert.Empty(t, fragment)
}

func TestCatalog_SystemPromptFragment_Error(t *testing.T) {
	t.Parallel()
	c := NewCatalog(&failingDiscoverer{})
	fragment := c.SystemPromptFragment(context.Background())
	assert.Empty(t, fragment)
}

func TestCatalog_SystemPromptFragment_PartialFailure(t *testing.T) {
	t.Parallel()
	good := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "good-skill", Description: "from good discoverer"},
		},
	}
	bad := &failingDiscoverer{}
	c := NewCatalog(good, bad)
	fragment := c.SystemPromptFragment(context.Background())

	assert.Contains(t, fragment, "good-skill")
	assert.Contains(t, fragment, "from good discoverer")
}

func TestCatalog_SetDirective(t *testing.T) {
	t.Parallel()
	d := &mockDiscoverer{
		meta: []SkillMeta{
			{Name: "alpha", Description: "first skill"},
		},
	}
	c := NewCatalog(d)
	c.SetDirective("Custom directive text.")
	fragment := c.SystemPromptFragment(context.Background())

	expected := "Custom directive text.\n\n- alpha: first skill"
	assert.Equal(t, expected, fragment)
}
