# Example Reference

This file is shipped as a reference fixture for the `writing-skills` built-in
skill. The loader picks it up at init time and stores it at the
skill-relative path `references/example.md`.

A real `writing-skills` SKILL.md can mention this in its References section
with a relative markdown link:

> "See [Example Reference](./references/example.md) for an example."

The LLM resolves that link to `read_skill(name="writing-skills",
path="references/example.md")` and receives this content.