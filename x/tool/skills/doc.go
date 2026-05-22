// Package skills implements progressive disclosure of agent skills for the ore
// framework, following the agentskills.io standard.
//
// A skill is a knowledge artifact — a SKILL.md file with YAML frontmatter
// (name, description, and optional metadata) followed by Markdown instructions.
// Progressive disclosure has three stages:
//
//   1. Discovery: The LLM sees only name + description via list_skills or
//      search_skills.
//   2. Activation: The LLM reads the full SKILL.md content via read_skill.
//   3. Execution: The LLM runs scripts from the skill's scripts/ directory
//      using other tools or its own reasoning. This stage is outside the scope
//      of this package.
//
// The package provides two Discoverer implementations — FSDiscoverer for the
// local filesystem and EmbeddedDiscoverer for an embed.FS — and a Catalog that
// aggregates, deduplicates (first-wins), caches, and searches across them.
//
// Composition
//
// Applications compose the skills toolkit into a tool.Registry:
//
//	tk := skills.NewToolkit(
//	    skills.NewFSDiscoverer(".agents/skills"),
//	)
//	tk.Register(registry)
//
// The application is responsible for telling the LLM about the skills tool
// (e.g., via system prompt) so the LLM knows to call list_skills. The skills
// tool itself is a single provider tool with three callable functions;
// individual skills are not registered as separate tools.
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
