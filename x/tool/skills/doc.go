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
//   2. Activation: The LLM reads the full SKILL.md content via read_skill
//      when it decides a skill is relevant.
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
//	    skills.NewFSDiscoverer(".agents/skills"),
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
package skills
