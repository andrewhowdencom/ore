---
name: github-issues
description: >-
  How to create and label GitHub issues for the ore repository.
  Load this skill whenever you need to open a new issue.
---

# GitHub Issues

## When to Use This Skill

Load this skill whenever you are about to create a new GitHub issue in the
`andrewhowdencom/ore` repository.

## Labeling Rules

Apply labels based on the issue type and scope. These labels are mandatory
where they match.

### 1. Plan Issues

If the issue is a **plan** — a design document, task list, or work breakdown
for a planned feature or refactor — label it:

```
plan
```

A plan issue is typically a task list that links to implementation PRs or
describes a multi-step change. Use the same label regardless of whether the
plan also has a second label (e.g. `conduit:http`).

### 2. Conduit Issues

If the issue is associated with a specific **conduit** (an I/O adapter under
`x/conduit/<name>/`), label it with the conduit prefix:

| Conduit | Label |
|---|---|
| HTTP | `conduit:http` |
| TUI | `conduit:tui` |
| Future conduits | `conduit:<name>` |

Use the label that matches the exact directory name under `x/conduit/`.

Examples:

- A bug in the HTTP conduit's SSE streaming → `conduit:http`
- A design plan for a new Slack conduit → `conduit:slack` + `plan`

## What NOT to Label

Do not add labels for:

- Core packages (`artifact/`, `state/`, `provider/`, `loop/`, `junk/`)
- General tooling (Taskfile, Go version bumps, linting)
- Documentation-only updates (unless they are a plan)

Keep the label set minimal; only use `plan` and `conduit:<name>` when they
apply.
