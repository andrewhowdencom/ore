# Development

This repository serves as a template for new projects. Developing in this repository primarily involves updating the **skills** that agents follow.

## Skill Structure

The configuration is organized into "Skills" in the `skills/` directory, strict following the [Agent Skills](https://agentskills.io) standard.

### Directory Layout
```
skills/<name>/
├── SKILL.md       # Entry point + Inlined Expertise (SOPs)
├── references/    # Detailed docs, templates, deep-dives
├── scripts/       # Optional automation tools
└── assets/        # Static files
```

### Philosophy: Inlined Expertise
When creating or editing skills, follow the **Inlined Expertise** philosophy:
- **SKILL.md** is NOT just an index. It MUST contain the most frequent "Standard Operating Procedures" (SOPs).
- **Do not** bury critical instructions in `references/`.
- **Do** use `references/` for explanations, history, or large templates.

**Example of Inlined Expertise (in SKILL.md):**
> "Run `go test -race ./...` to verify changes." (Direct instruction)

**Example of Reference:**
> "See `references/testing_philosophy.md` for why we use Table-Driven tests." (Deep dive)

## Workflow

1.  **Identify the skill:** Determine the scope (e.g., `git`, `go`).
2.  **Edit `SKILL.md`:** Add actionable instructions directly.
3.  **Add References:** Create files in `references/` only if complexity demands it.
4.  **Validate:** Ensure markdown renders correctly and links work.