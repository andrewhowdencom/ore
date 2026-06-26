---
name: writing-skills
description: Guidelines for creating, reviewing, and improving agent skills.
---

# Writing Skills

This skill governs how agent skills are authored and audited. It contains two paths: a **Creation** path for new skills, and a **Review** path for existing skills. The Inlined Expertise doctrine from `DEVELOPMENT.md` applies — critical SOPs live in `SKILL.md`, not in `references/`.

## Creation Path

### 1. Elicit
Ask 1–3 high-value questions before drafting. Probe intent, scope, target audience, prior attempts, and related skills. Avoid open-ended questions that invite the user to dump context; ask for the specific decision-blocking information.

### 2. Tentative Frame
Propose a hypothesis about scope, name, and shape. Give the user something concrete to disagree with. Frame as a starting point, not a finished design.

### 3. Refine
Iterate: propose → correct → revise. Show what changed between iterations so the user can audit the reasoning. Each iteration should narrow scope or sharpen wording, not expand.

### 4. Converge
Land on a 1–2 sentence problem statement, explicit scope boundaries, and at least one acknowledged constraint. State the proposed skill name, target repo, and parent directory.

### 5. Confirm and Draft
Ask the user: "Want me to draft the SKILL.md?" Do not write the file until they confirm. When they do, draft to the agreed location and present the result.

## Review Path

When asked to review, audit, check, or improve an existing `SKILL.md`, run the gates below against it. Output enumerated findings only; do not render pass/fail verdicts. The user decides what to act on.

### 1. Frontmatter
- `name` is lowercase-hyphenated and matches the parent directory name exactly.
- `description` is a single declarative sentence stating what the skill does and when to use it.
- No extraneous fields. This repo's house style uses only `name` and `description` — no `license`, `metadata`, `compatibility`, or `allowed-tools`.

### 2. Trigger Quality
- Would the description fire on the right user prompts? Test against hypothetical phrasings ("create a skill for X", "review the X skill", "improve X").
- Could the description undertrigger on implicit requests? If so, surface that as a finding; do not silently expand the description.

### 3. Length and Shape
- Body is under ~150 lines. If longer, offload detail to `references/`.
- Title is `# <Capitalized Skill Name>`.

### 4. Tone and Rationale
- Imperative voice throughout.
- Every MUST/NEVER is paired with a *why* so the model can reason about edge cases rather than blindly obey. Bare "NEVER do X" without explanation is a finding.

### 5. Scope Tightness
- The skill addresses a single concern. Kitchen-sink skills that try to cover multiple domains are a finding.

### 6. References
- Linked files exist, use relative paths from the skill root, and are one-level deep (no nested chains).

## Definition of Done

A skill is complete when:

- It contains the most frequent SOPs directly in `SKILL.md` — critical instructions are not buried in `references/`.
- It matches the conventions of sibling skills in the same repo (frontmatter shape, length budget, tone).
- The Review path's six gates pass when applied to the new skill itself.