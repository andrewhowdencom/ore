---
name: _example
description: Placeholder built-in skill shipped with the framework so the //go:embed directive compiles. Replace this file with a real built-in skill.
---

# Built-in Skills Placeholder

This `_example` skill exists only so `//go:embed builtin` resolves at
compile time before any real built-in skill is added. It is not intended
to be useful on its own.

## How to add a real built-in skill

Create a subdirectory under `x/tool/skills/builtin/` with a `SKILL.md`
file. The file must have YAML frontmatter with `name` and `description`
fields, followed by the markdown body.

For example, to add a skill named `foo`:

```text
x/tool/skills/builtin/foo/SKILL.md
```

with content:

```yaml
---
name: foo
description: A real built-in skill shipped with the framework.
---

# Foo

Skill body here.
```

The skill is automatically discovered at package init and added to
`BuiltInSkills`, where applications can access it via
`skills.BuiltIn("foo")` or by passing `skills.BuiltInSkills` to
`skills.NewToolkit`.

## How to remove this placeholder

Delete the `x/tool/skills/builtin/_example/` directory. As long as at
least one `SKILL.md` remains under `x/tool/skills/builtin/`, the
`//go:embed` directive continues to compile.
