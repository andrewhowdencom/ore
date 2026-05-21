# Workshop — Terminal Coding Assistant

Workshop is a domain-specific Forge blueprint example: a terminal-based
coding assistant that helps users write, review, refactor, and debug code
across any language or framework.

Unlike the generic TUI and HTTP examples, Workshop leverages Forge's
transform system to inject a coding-specific assistant identity and
behavioral guardrails through transforms defined in the blueprint. It is designed as
a concrete, non-trivial use case to stress-test Forge's declarative
expressiveness and to validate each new Forge extension by making
workshop measurably more capable.

## Current Capabilities

- **TUI conduit** — single-session terminal chat interface.
- **System prompt transform** (`x/systemprompt`) — injects a coding-specific
  persona as a virtual `RoleSystem` turn ("You are a terminal-based coding
  assistant...") into every inference turn via `VirtualTurnState`, without
  mutating the persistent conversation buffer.
- **Guardrails transform** (`x/guardrails`) — injects behavioral rules
  (markdown formatting, conciseness, rationale for changes) as virtual
  `RoleUser` turns.
- **Empty handler wiring** — the `handlers: []` array in the blueprint is a
  structural placeholder. Forge supports handler module wiring, but no
  handler module in the repository currently exports the `New`/`OptionsFromMap`
  contract required by the generated template.

## Build & Run

```bash
cd examples/forge/workshop
go run ../../../cmd/forge build --config forge.yaml
./workshop
```

The binary reads the standard `ORE_*` environment variables:

- `ORE_API_KEY` — required
- `ORE_MODEL` — defaults to `gpt-4o`
- `ORE_BASE_URL` — optional, for custom OpenAI-compatible endpoints
- `ORE_STORE_DIR` — optional, enables persistent JSON thread store
- `ORE_CONDUIT_TUI_THREAD_ID` — optional, resume an existing thread

## Milestone Roadmap

| Milestone | Forge / Framework Capability Required | Workshop Capability Unlocked |
|---|---|---|
| **M1: Custom prompt** | ✅ `transforms:` array + `x/systemprompt` | Assistant has coding-specific identity |
| **M2: Tool handler wiring** | Forge `handlers:` array with a module exporting `New`/`OptionsFromMap` | Tool handler is wired into the loop |
| **M3: Tool registry declarations** | YAML-native tool schema/function declarations, or a pre-registered handler module | Assistant can read files, run bash |
| **M4: File edit tool** | M2 + M3 + custom artifact rendering in TUI | Assistant can propose and apply edits |
| **M5: HTTP conduit** | Existing multi-conduit support | Web interface alongside TUI |
| **M6: Provider selection** | Blueprint `provider:` stanza or app-package extension | Use Anthropic, local, or other adapters |
| **M7: Cognitive pattern selection** | Blueprint `cognitive:` stanza or app-package extension | ReAct or custom loops for complex tasks |

## Discovered Gaps

These are the Forge / framework limitations that still block workshop
from becoming a fully autonomous coding assistant. Each should become a
separate design issue, validated by workshop adopting it.

1. **No Forge-compatible handler module for tools** — `x/tool` exports
   `NewRegistry` and `Registry.Handler()`, but Forge templates require
   `func New(opts ...Option) (loop.Handler, error)` and
   `func OptionsFromMap(map[string]any) ([]Option, error)`.
2. **No tool registry population in blueprints** — Even with a compatible
   handler module, coding assistant tools (read file, run bash, edit code)
   require a `tool.Registry` populated with Go function implementations.
   There is no YAML-native way to declare tools, schemas, or functions.
3. **No provider selection in blueprints** — The `app` package hardcodes
   the OpenAI provider adapter. No blueprint field exists to select
   Anthropic, local, or other adapters.
4. **No cognitive pattern selection in blueprints** — The `app` package
   hardcodes a simple turn processor (`step.Turn`). There is no way to
   request `cognitive.ReAct` or a custom loop.
