// Package skills implements progressive disclosure of agent skills for the ore
// framework, following the agentskills.io standard.
//
// A skill is a knowledge artifact — a SKILL.md file with YAML frontmatter
// (name, description, and optional metadata) followed by Markdown instructions.
// Progressive disclosure has two stages:
//
//   1. Discovery: The LLM sees the full skill catalog eagerly injected into
//      the system prompt via SystemPromptFragment. This includes the name and
//      description of every discovered skill, plus a behavioral directive.
//   2. Activation: The LLM reads the canonical SKILL.md or any reference
//      file via read_skill when it decides a skill is relevant. Reference
//      files live under a skill's `references/` directory per the
//      agentskills.io convention and are served through the same tool with
//      an optional `path` parameter.
//
// Execution (running scripts from a skill's scripts/ directory using other tools
// or the LLM's own reasoning) is outside the scope of this package.
//
// The package provides two Discoverer implementations — FSDiscoverer for the
// local filesystem and EmbeddedDiscoverer for an embed.FS — and a Catalog that
// aggregates, deduplicates (first-wins), and caches them.
//
// Composition
//
// Applications compose the skills toolkit into a tool.Registry:
//
//	tk := skills.NewToolkit(
//	    skills.BuiltInSkills,                       // framework-shipped skills
//	    skills.NewFSDiscoverer(".agents/skills"),   // repo-local skills
//	)
//	if err := tk.Register(registry); err != nil {
//	    ...
//	}
//
// Register adds only the read_skill tool. Discovery happens automatically
// through the system prompt when the application wires SystemPromptFragment()
// into systemprompt.New(). Individual skills are not registered as separate
// tools.
//
// Reading Skill Files
//
// read_skill has two parameters: a required `name` (the skill) and an
// optional `path` (skill-relative, forward-slash). With path omitted, the
// canonical SKILL.md is returned. With path set, the file at that location
// is returned (e.g., path="references/foo.md"). Both forms are served by
// the same discoverer abstraction, so filesystem, embedded, and any
// future remote discoverer all handle references uniformly:
//
//	// SKILL.md
//	result, _ := tk.ReadSkill(ctx, nil, map[string]any{"name": "go"})
//
//	// Reference file
//	result, _ := tk.ReadSkill(ctx, nil, map[string]any{
//	    "name": "go",
//	    "path": "references/testing_philosophy.md",
//	})
//
// System Prompt Integration
//
// Applications should proactively inject a formatted listing of all discovered
// skills into the system prompt so the LLM knows what is available without
// calling a discovery tool:
//
//	import "github.com/andrewhowdencom/ore/x/systemprompt"
//
//	sp, _ := systemprompt.New(
//	    systemprompt.WithContextContentFunc(tk.SystemPromptFragment()), // returns func(context.Context) string
//	)
//
// The default fragment includes a strong behavioral directive that tells the
// LLM when to load skills — for example: "When your task matches a skill
// description below, call read_skill(name=<skill>) to load its detailed
// instructions before proceeding." This is followed by a deterministic bullet
// list of skill names and descriptions.
//
// Applications can customize the directive via SetDirective on either Catalog
// or Toolkit:
//
//	tk.SetDirective("When you need domain expertise, load the relevant skill.")
//
// If no skills are discovered or discovery fails, the fragment returns an empty
// string and the section is omitted from the prompt.
//
// Deduplication and Overrides
//
// When multiple discoverers expose skills with the same name, the first
// discoverer wins. A warning is logged when duplicates are detected.
//
// Malformed Skills
//
// SKILL.md files missing required frontmatter fields (name, description) are
// skipped during discovery with a warning log rather than failing the entire
// catalog.
//
// Built-in Skills
//
// The framework ships a small set of skills as part of the package itself.
// They are exposed as a StaticSource named BuiltInSkills and a lookup
// helper BuiltIn:
//
//	if sk, ok := skills.BuiltIn("write-skill"); ok {
//	    fmt.Println(sk.Content)
//	}
//
// BuiltInSkills satisfies the Discoverer interface, so it composes with
// other sources:
//
//	tk := skills.NewToolkit(skills.BuiltInSkills, skills.NewFSDiscoverer(".agents/skills"))
//
// BuiltInSkills is populated at package init from .md files under
// x/tool/skills/builtin/. Each subdirectory contains a SKILL.md file in
// the same agentskills.io format used by .agents/skills/:
//
//	x/tool/skills/builtin/<name>/SKILL.md
//
// Add a new built-in skill by creating such a directory; remove one by
// deleting it. The init() loader walks the embedded directory at startup
// and skips malformed files with a logged warning — the loader never
// panics and the registry is empty (rather than missing) if every file
// is malformed.
//
// In addition to SKILL.md, the loader picks up files under each skill's
// `references/` directory and populates the skill's References map:
//
//	x/tool/skills/builtin/<name>/references/foo.md
//
// A file at that path is stored at the skill-relative key
// `references/foo.md` and served through read_skill(name, path). Reference
// files are not advertised in the system prompt — the LLM discovers them
// by reading SKILL.md and following its markdown links.
//
// When a built-in skill has the same name as a user-discovered skill,
// the first discoverer passed to NewToolkit wins (Catalog's existing
// first-wins deduplication). To keep built-ins authoritative, pass
// BuiltInSkills first.
package skills
