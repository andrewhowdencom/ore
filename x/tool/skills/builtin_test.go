package skills

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltInSkills_LoadsPlaceholder(t *testing.T) {
	t.Parallel()
	// BuiltInSkills is populated by init(). We do not assert an exact
	// count because future real skills may be added. We assert that
	// the writing-skills skill (the first real built-in) is present.
	require.NotEmpty(t, BuiltInSkills, "BuiltInSkills must contain at least one entry")

	found := false
	for _, sk := range BuiltInSkills {
		if sk.Name == "writing-skills" {
			found = true
			assert.NotEmpty(t, sk.Description, "writing-skills description must be non-empty")
			assert.NotEmpty(t, sk.Content, "writing-skills content must be non-empty")
			break
		}
	}
	assert.True(t, found, "writing-skills must be present in BuiltInSkills")
}

func TestBuiltIn_KnownName(t *testing.T) {
	t.Parallel()
	sk, ok := BuiltIn("writing-skills")
	require.True(t, ok, "BuiltIn must find writing-skills")
	assert.Equal(t, "writing-skills", sk.Name)
	assert.NotEmpty(t, sk.Content)
}

func TestBuiltIn_UnknownName(t *testing.T) {
	t.Parallel()
	sk, ok := BuiltIn("does-not-exist")
	assert.False(t, ok)
	assert.Equal(t, Skill{}, sk)
}

func TestBuiltInSkills_DiscoverableViaDiscoverer(t *testing.T) {
	t.Parallel()
	metas, err := BuiltInSkills.Discover(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, metas)
}

func TestLoadBuiltin_LoadsValidSkills(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"skills/alpha/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: alpha\ndescription: first\n---\n\n# Alpha\n"),
		},
		"skills/beta/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: beta\ndescription: second\n---\n\n# Beta\n"),
		},
	}
	out := loadBuiltin(fsys)
	require.Len(t, out, 2)

	byName := map[string]Skill{}
	for _, sk := range out {
		byName[sk.Name] = sk
	}
	assert.Equal(t, "---\nname: alpha\ndescription: first\n---\n\n# Alpha\n", byName["alpha"].Content)
	assert.Equal(t, "---\nname: beta\ndescription: second\n---\n\n# Beta\n", byName["beta"].Content)
}

func TestLoadBuiltin_SkipsMalformed(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"skills/good/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: good\ndescription: valid\n---\n\n# Good\n"),
		},
		"skills/bad/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nnot-name: bad\n---\n\n# Bad\n"),
		},
		"skills/also-good/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: also-good\ndescription: also valid\n---\n\n# Also Good\n"),
		},
		"skills/non-skill.txt": &fstest.MapFile{
			Data: []byte("not a skill"),
		},
	}
	out := loadBuiltin(fsys)
	require.Len(t, out, 2)

	byName := map[string]Skill{}
	for _, sk := range out {
		byName[sk.Name] = sk
	}
	assert.Contains(t, byName, "good")
	assert.Contains(t, byName, "also-good")
	assert.NotContains(t, byName, "bad")
}

func TestLoadBuiltin_Empty(t *testing.T) {
	t.Parallel()
	out := loadBuiltin(fstest.MapFS{})
	assert.Empty(t, out)
}