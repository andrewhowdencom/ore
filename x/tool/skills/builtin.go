package skills

import (
	"embed"
	"io/fs"
	"log/slog"
	"path"
)

// builtinFS holds the SKILL.md files shipped with the framework. Each
// subdirectory of builtin/ contains exactly one SKILL.md file, named
// after the skill (or with a leading underscore for framework scaffolds).
//
// The all: prefix is required because Go's default embed behavior skips
// files and directories whose names start with '.' or '_'. The leading
// underscore convention is used here to mark framework scaffolding (e.g.
// the _example placeholder) so it sorts last and is visually distinct.
//
//go:embed all:builtin
var builtinFS embed.FS

// BuiltInSkills is the registry of framework-shipped skills, populated at
// package init from .md files under builtin/. Use it directly as a
// Discoverer (it satisfies the interface via StaticSource), fetch by
// name with BuiltIn, or iterate to enumerate.
//
// BuiltInSkills is read-only after init; concurrent access is safe.
var BuiltInSkills StaticSource

func init() {
	BuiltInSkills = loadBuiltin(builtinFS)
}

// loadBuiltin walks fsys, parses every SKILL.md it finds, and returns the
// valid entries. Malformed files, read errors, and unreadable paths are
// logged via slog.Warn and skipped; loadBuiltin itself never returns an
// error and never panics.
func loadBuiltin(fsys fs.FS) StaticSource {
	var out StaticSource
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("skipping unreadable path during built-in skill discovery", "path", p, "error", err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if path.Base(p) != "SKILL.md" {
			return nil
		}

		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			slog.Warn("skipping unreadable built-in skill file", "path", p, "error", err)
			return nil
		}

		skill, err := parseSkill(data)
		if err != nil {
			slog.Warn("skipping malformed built-in SKILL.md", "path", p, "error", err)
			return nil
		}
		out = append(out, skill)
		return nil
	})
	if err != nil {
		// fs.WalkDir on an embed.FS should not fail under normal
		// circumstances, but log if it does.
		slog.Error("failed to walk built-in skills directory", "error", err)
	}
	return out
}

// BuiltIn returns the named skill from the registry and a bool indicating
// whether it was found. BuiltIn does a linear scan; it is intended for
// occasional lookups, not hot paths.
func BuiltIn(name string) (Skill, bool) {
	for _, sk := range BuiltInSkills {
		if sk.Name == name {
			return sk, true
		}
	}
	return Skill{}, false
}