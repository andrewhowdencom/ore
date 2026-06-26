package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkill_Meta(t *testing.T) {
	t.Parallel()
	sk := Skill{
		Name:        "alpha",
		Description: "first skill",
		Content:     "# Alpha\n\nbody",
	}
	assert.Equal(t, SkillMeta{Name: "alpha", Description: "first skill"}, sk.Meta())
}

func TestStaticSource_Discover(t *testing.T) {
	t.Parallel()
	src := StaticSource{
		{Name: "alpha", Description: "first skill", Content: "# Alpha"},
		{Name: "beta", Description: "second skill", Content: "# Beta"},
	}
	metas, err := src.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []SkillMeta{
		{Name: "alpha", Description: "first skill"},
		{Name: "beta", Description: "second skill"},
	}, metas)
}

func TestStaticSource_Discover_Empty(t *testing.T) {
	t.Parallel()
	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		metas, err := StaticSource(nil).Discover(context.Background())
		require.NoError(t, err)
		assert.Empty(t, metas)
	})
	t.Run("empty slice", func(t *testing.T) {
		t.Parallel()
		metas, err := StaticSource{}.Discover(context.Background())
		require.NoError(t, err)
		assert.Empty(t, metas)
	})
}

func TestStaticSource_Read(t *testing.T) {
	t.Parallel()
	src := StaticSource{
		{Name: "alpha", Description: "first skill", Content: "# Alpha body"},
		{Name: "beta", Description: "second skill", Content: "# Beta body"},
	}
	content, err := src.Read(context.Background(), "beta")
	require.NoError(t, err)
	assert.Equal(t, "# Beta body", content)
}

func TestStaticSource_Read_NotFound(t *testing.T) {
	t.Parallel()
	src := StaticSource{{Name: "alpha", Description: "first", Content: "x"}}
	_, err := src.Read(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"missing"`)
	assert.Contains(t, err.Error(), "not found")
}

func TestStaticSource_SatisfiesDiscoverer(t *testing.T) {
	t.Parallel()
	// The compile-time assertion in static.go guarantees StaticSource
	// implements Discoverer. This test exercises the runtime behavior
	// through the interface to confirm.
	var d Discoverer = StaticSource{{Name: "x", Description: "y", Content: "z"}}
	metas, err := d.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, "x", metas[0].Name)

	content, err := d.Read(context.Background(), "x")
	require.NoError(t, err)
	assert.Equal(t, "z", content)
}