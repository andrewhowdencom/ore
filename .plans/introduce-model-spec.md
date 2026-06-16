# Plan: Introduce `models.ModelSpec` for typed model identity and inference configuration

Related GitHub issue: [#451](https://github.com/andrewhowdencom/ore/issues/451).
Supersedes the per-invocation string-override work from #435.
Likely obsoletes #445 (Model Defaults) once `ModelSpec` lands.

## Objective

Replace the bare-string model identity (`provider.WithModel(name)`) with a
first-class `models.ModelSpec` value type — combining model identity with
inference configuration (context window, temperature, thinking level, etc.) —
as the canonical argument to provider invocations. Per-vendor catalogs of
well-known specs cover the p80 case; ad-hoc construction covers the long
tail. The `Provider` interface takes a `ModelSpec` as a first argument; the
loop supports a configured default `ModelSpec` with per-call full-Spec
overrides; the compactor has its own independently-configured `ModelSpec`.

This is a backwards-incompatible change. The string-override primitive from
#435 is removed; the catalog-of-defaults intent from #445 is folded into the
per-vendor model subpackages.

## Context

### Discovery findings

**Core package topology (root module, stdlib-only):**
- `provider/provider.go` (194 lines) — defines `Provider` interface,
  `InvokeOption` marker interface, and the per-invocation option types
  (`ToolsOption`, `ModelOption`, `MaxTokensOption`, `ThinkingLevel`).
- `loop/loop.go` (614 lines) — `Step.Turn(ctx, st, p, opts...)` and
  `Step.Submit(ctx, st, role, arts...)`. `Step` has functional options
  `WithTransforms`, `WithHandlers`, `WithOnEmit`, `WithState`,
  `WithInvokeOptions`, `WithTracer`.
- `loop/interfaces.go` — exposes `TurnRunner` and `TurnSubmitter` interfaces.
- `loop/pipeline.go` — `Pipeline.Turn` merges pre-bound `invokeOpts` (from
  `WithInvokeOptions`) with per-call `opts`, then calls
  `prov.Invoke(ctx, st, provCh, allOpts...)`.
- `state/state.go` — `State` interface, `Turn` struct (role + artifacts +
  timestamp), `Role` constants.
- `artifact/artifact.go` — `Usage` struct carries `PromptTokens`,
  `CompletionTokens`, `TotalTokens`, etc.; `StopReasonKind` enum.

**Provider adapters (under `x/provider/<vendor>/`, sub-modules):**
- `x/provider/openai/openai.go` (1073 lines) — `Invoke` walks `opts` and
  extracts `tools`, `temperature`, `thinkingLevel`, `maxTokens`, `sessionID`,
  `cacheControl`, `modelName` via type assertions. Resolves `effectiveModel`
  from `modelName` (per-call) with fallback to `p.model` (constructor).
  Sets the OTel span attribute `"model"` to `effectiveModel` so the trace
  reflects the actual model that served the request.
- `x/provider/anthropic/anthropic.go` (1014 lines) — symmetric structure;
  the `Invoke` method has the same `effectiveModel` precedence pattern.
- Both providers have `WithModel(string)` constructor option (sets the
  constructor default) and honor `provider.ModelOption` per-invocation.

**Existing per-invocation override plumbing (from PR #436 / #435):**
- `provider.WithModel(name string) InvokeOption` — bare-string override.
- `provider.ModelOption` — the option type. `Model == ""` is a no-op;
  constructor model still wins.
- `session/stream.go` — `Stream.ModelOption()` reads
  `Thread.Metadata["provider.model"]` and returns the `provider.InvokeOption`
  (or `nil` if absent/empty). Comment: *"The metadata key `provider.model` is
  the framework contract."*
- `x/tool/set_model/setmodel.go` — a slash-only command
  (`/model <name>`) that writes the metadata key via
  `stream.SetMetadata("provider.model", name)`. Slash-only, **not** a tool —
  the LLM must not change its own model.

**Compaction (`x/compaction/`):**
- `compaction.go` (125 lines) — `Trigger` and `Strategy` interfaces;
  `Compactor` orchestrates them. `TokenUsageTrigger{MaxTokens int}` fires
  when the latest `artifact.Usage` reports `TotalTokens > MaxTokens`. The
  `MaxTokens` is **hard-coded by the application** at trigger construction;
  the framework has no way to read the model's actual window.
- `summarize.go` (201 lines) — `SummarizeStrategy{Provider, Prompt, MaxTokens}`
  calls `s.Provider.Invoke(ctx, buf, ch, opts...)` directly with
  `provider.WithMaxTokens(s.MaxTokens)`. The strategy and the trigger are
  configured **independently**; the application is responsible for keeping
  them in sync with whatever model the compactor is using.

**Convention from `AGENTS.md`:**
- "Core packages (`artifact/`, `state/`, `provider/`, `loop/`, `tool/`, etc.)
  live at the root level so external applications can import them. Do not
  place framework contracts under `internal/`."
- "Root module's `go.mod` must stay stdlib-only."
- Concrete provider adapters live under `x/provider/<name>/`.
- "Aggressive refactoring — rename packages, move files, delete indirection,
  and break internal APIs when doing so produces cleaner module boundaries."

### Correction to the issue body — type vs. data

The issue body lumps everything into `x/models/`, but the *type* and
the *data* are different concerns with different homes:

- **The `Spec` and `ThinkingLevel` *types*** are *core primitives*.
  `Provider.Invoke` and the rest of the framework consume them as
  parameters. They must be importable by `provider/`, which lives in
  the root module, and the root module must stay stdlib-only per
  AGENTS.md. A sub-module under `x/models/` with its own `go.mod`
  cannot satisfy that — putting the *type* under `x/` would force the
  root to import a sub-module, violating "core primitives at the root."
  So **the type lives at `models/` (root level)**, alongside `state/`,
  `provider/`, `loop/`, `tool/`, `artifact/`.

- **The named `Spec` *values*** (e.g. `GPT4o`, `ClaudeOpus45`) are
  *extensions* — vendor-local data describing individual models.
  They live under `x/provider/<vendor>/models/` as subpackages of the
  existing provider modules. **The type is shared; the catalog of
  values is vendor knowledge.**

### Design decisions settled in the ideation conversation

1. **`ModelSpec` taxonomy**: identity (name) + inference configuration
   (window, max output tokens, temperature, thinking level, top_p, top_k,
   seed, stop sequences, frequency/presence penalties). Capabilities
   (audio, video, image, tool support) are a separate, deferred axis.
2. **Provider is the vendor, not the model.** A provider knows its wire
   format; a `ModelSpec` is portable across providers that can serve the
   named model.
3. **Per-vendor catalogs** in `x/provider/<vendor>/models/` with the
   p80 cases (`openaimodels.GPT4o`, `anthropicmodels.ClaudeOpus45`, …).
   Ad-hoc construction covers the long tail.
4. **Defaulting is layered**: loop default + per-call full `ModelSpec`
   override (per-call wins). The session is the *deriver* of the per-call
   Spec from its metadata; the framework doesn't merge metadata-to-spec.
5. **Compactor has its own `ModelSpec`** — first instance of the
   sub-agent pattern. Independently configured, may use a different (cheaper)
   model than the main loop. The compactor's `TokenUsageTrigger` reads
   `Spec.Window`.
6. **Break BC.** The bare-string `WithModel(name)` and `provider.ModelOption`
   are removed; `Provider.Invoke` takes `ModelSpec` as a first argument;
   the loop's `Turn` signature changes to accept a `ModelSpec`.

## Architectural Blueprint

**Selected path**: introduce a root-level `models` package exporting a
`ModelSpec` value type; per-vendor `models/` subpackages export
well-known values; the `Provider` interface and `loop.Step` take
`ModelSpec` as a first-class argument; the compactor takes its own
independently-configured `ModelSpec`; the session derives the per-call
Spec from its metadata.

**Why this and not alternatives**:
- A `provider.Info()` method on the provider would couple transport with
  knowledge. The provider's job is to talk to a vendor; the model's
  identity and configuration are a separate concern. (Rejected.)
- Keeping the model as a bare string and stuffing config into separate
  `InvokeOption`s is the current shape and exactly what the user wants
  to escape: every new field adds another option type, another
  type-assertion in every adapter, and another ad-hoc translation in
  the wire-format code. (Rejected.)
- A per-vendor `models` subpackage (not a single shared catalog) keeps
  vendor knowledge in vendor packages. OpenAI's `gpt-4o` is not
  Anthropic's `claude-opus-4-5`; the catalogs are vendor-local by
  necessity. The *type* is shared; the *values* are vendor-local.
  (Selected.)

**The shape**:

```go
// models/models.go (new, root level, stdlib-only)
package models

// Spec is a value object combining a model identity with its inference
// configuration. It is the canonical argument to provider invocations.
type Spec struct {
    // Identity
    Name string

    // Inference configuration. Pointer fields encode "not set";
    // zero values are framework defaults where applicable.
    Window           int
    MaxOutputTokens  int64
    Temperature      *float64
    ThinkingLevel    ThinkingLevel  // moved from provider package
    TopP             *float64
    TopK             *int
    Seed             *int64
    StopSequences    []string
    FrequencyPenalty *float64
    PresencePenalty  *float64
}

// ThinkingLevel is a portable, qualitative description of how much
// reasoning effort a model should spend. Moved here from provider/ so
// that models.Spec can hold it without creating a cycle.
type ThinkingLevel string

const (
    ThinkingLevelOff     ThinkingLevel = "off"
    ThinkingLevelMinimal ThinkingLevel = "minimal"
    ThinkingLevelLow     ThinkingLevel = "low"
    ThinkingLevelMedium  ThinkingLevel = "medium"
    ThinkingLevelHigh    ThinkingLevel = "high"
    ThinkingLevelMax     ThinkingLevel = "max"
)

// (Valid, ParseThinkingLevel, etc. — same as today, package moved.)
```

```go
// x/provider/openai/models/models.go (new subpackage)
package openaimodels

import "github.com/andrewhowdencom/ore/models"

// GPT4o is the OpenAI GPT-4o spec.
var GPT4o = models.Spec{
    Name:             "gpt-4o",
    Window:           128_000,
    MaxOutputTokens:  16_384,
    Temperature:      nil, // omit; default applies
    ThinkingLevel:    models.ThinkingLevelOff,
}

// GPT4oMini is the smaller, faster GPT-4o.
var GPT4oMini = models.Spec{
    Name:            "gpt-4o-mini",
    Window:          128_000,
    MaxOutputTokens: 16_384,
    ThinkingLevel:   models.ThinkingLevelOff,
}

// O1, O1Mini, O3, etc. — same pattern.
```

```go
// provider/provider.go (refactored)
package provider

// Invoke is the per-invocation entry point for an LLM provider.
// The Spec carries the model identity and inference configuration;
// the adapter translates it to its wire format. Adapters that do not
// recognize a Spec field must leave it on the floor — the Spec is
// permissive by design (fields are independent).
type Provider interface {
    Invoke(ctx context.Context, s state.State, spec models.Spec, ch chan<- artifact.Artifact, opts ...InvokeOption) error
}

// WithModel and ModelOption are removed. Spec is the only way to
// pass model identity and configuration.

// (ToolsOption, MaxTokensOption, InvokeOption marker interface — unchanged.)
```

```go
// session/stream.go (refactored)
// Spec returns the effective ModelSpec for the next turn, derived from
// the thread's metadata. A nil result means "use the loop's default";
// a non-nil result replaces the loop's default entirely (no field-level
// merge). The framework doesn't merge metadata into a base Spec; that's
// the session's responsibility (see deriveSpec).
func (s *Stream) Spec() (models.Spec, bool) { ... }

// deriveSpec reads Thread.Metadata and constructs a Spec. The metadata
// keys are private to this package; the slash command in
// x/tool/set_model writes them.
```

```go
// x/compaction/compaction.go (refactored)
// TokenUsageTrigger fires when the most recent artifact.Usage in the
// turn slice indicates total tokens exceed the Spec's window.
type TokenUsageTrigger struct {
    Spec models.Spec // Window read from Spec.Window
}
```

```go
// x/compaction/summarize.go (refactored)
type SummarizeStrategy struct {
    Provider provider.Provider
    Spec     models.Spec  // independently configured; may be a different
                          // model from the main loop's
    Prompt   string
    // MaxTokens is folded into Spec.MaxOutputTokens.
}
```

**Architectural decisions left to the implementer** (call out in tasks):

- Pointer vs zero-value fields on `Spec`: pointer for "not set" is the
  cleaner Go idiom but adds nil-checks everywhere. Zero-value with
  "zero is a no-op" semantics (matching `provider.ModelOption.Model == ""`)
  is simpler. **Default to pointer for non-default-zero-meaningful fields
  (Temperature, TopP, TopK, Seed, FrequencyPenalty, PresencePenalty);
  zero-value for fields where zero is genuinely a no-op (MaxOutputTokens,
  StopSequences).** Builder methods (`Spec.WithTemperature(0.7)`) are out
  of scope for v1.
- Whether `Models()` (a registry function returning a `[]Spec`) is shipped
  per-vendor in addition to the named constants. The named constants are
  the primary surface; the registry is convenient for tooling (e.g. a
  `/models` slash command) but is not required. **Ship constants in v1;
  add a registry later if a consumer needs it.**
- Where the `provider.ThinkingLevel` constants live after the move. Two
  options: (a) re-export from `provider` as type aliases for back-compat
  with existing call sites in the adapters; (b) hard-break — adapters
  import the new `models` package directly. AGENTS.md says break BC, so
  (b) is the consistent choice. **Hard-break: `provider.ThinkingLevel`
  is removed; adapters import `models.ThinkingLevel` directly.**

## Requirements

1. A new root-level package `models/` exists, stdlib-only, exporting
   `Spec`, `ThinkingLevel`, the existing `ThinkingLevel*` constants, and
   `ParseThinkingLevel`.
2. `provider.ThinkingLevel` and the `ThinkingLevel*` constants are
   removed from `provider/provider.go`; all references in the codebase
   are updated to use `models.ThinkingLevel`.
3. `provider.WithModel(string)` and `provider.ModelOption` are removed.
4. `Provider.Invoke` signature is `Invoke(ctx, state, spec models.Spec,
   ch, opts...)`. Both the OpenAI and Anthropic adapters are updated.
5. Each provider's `WithModel(string)` constructor option is removed.
6. `loop.Step.Turn` signature is `Turn(ctx, st, spec models.Spec, p, opts...)`.
7. `loop.Step` has a `WithDefaultSpec(spec models.Spec)` option for
   pre-binding a default; `WithInvokeOptions` is unchanged.
8. `loop.Pipeline.Turn` signature mirrors `Step.Turn`.
9. `session.Stream` exposes a `Spec() (models.Spec, bool)` method that
   derives the per-call Spec from `Thread.Metadata`. The exact metadata
   keys are private to the session package.
10. `x/tool/set_model/setmodel.go` is updated to write the new metadata
    keys consumed by `Stream.Spec()`.
11. `compaction.SummarizeStrategy` has a `Spec` field (not a hardcoded
    `MaxTokens int64`).
12. `compaction.TokenUsageTrigger` has a `Spec` field; it reads
    `Spec.Window` instead of accepting a `MaxTokens int`.
13. The per-vendor subpackages `x/provider/openai/models/` and
    `x/provider/anthropic/models/` exist, exporting named `Spec`
    constants for the p80 case (e.g. `GPT4o`, `GPT4oMini`,
    `ClaudeOpus45`, `ClaudeSonnet45`).
14. The full test suite passes with `-race`.
15. All example applications compile and run their integration tests.

## Task Breakdown

### Task 1: Create `models` package with `Spec` type

- **Goal**: Add a new root-level `models/` package, stdlib-only, exporting
  the `Spec` value type, the `ThinkingLevel` type (moved from `provider`),
  and the `ThinkingLevel*` constants. This is a leaf package; no
  internal ore imports.
- **Dependencies**: None.
- **Files Affected**: None (new package).
- **New Files**:
  - `models/doc.go` — package doc.
  - `models/spec.go` — `Spec` struct, `With*` accessor methods (optional
    for v1; see Architectural Blueprint).
  - `models/thinking.go` — `ThinkingLevel` type and constants, moved
    verbatim from `provider/provider.go`. Includes `Valid()` and
    `ParseThinkingLevel()`.
  - `models/spec_test.go` — table-driven tests for `Spec` zero-values,
    pointer-field semantics, and `ThinkingLevel.ParseThinkingLevel`.
- **Interfaces**:
  ```go
  // Spec is a value object combining a model identity with its
  // inference configuration.
  type Spec struct {
      Name             string
      Window           int
      MaxOutputTokens  int64
      Temperature      *float64
      ThinkingLevel    ThinkingLevel
      TopP             *float64
      TopK             *int
      Seed             *int64
      StopSequences    []string
      FrequencyPenalty *float64
      PresencePenalty  *float64
  }

  // ThinkingLevel is a portable, qualitative description of how much
  // reasoning effort a model should spend on a turn. Adapters translate
  // the level into their provider's wire format at request time.
  type ThinkingLevel string

  const (
      ThinkingLevelOff     ThinkingLevel = "off"
      ThinkingLevelMinimal ThinkingLevel = "minimal"
      ThinkingLevelLow     ThinkingLevel = "low"
      ThinkingLevelMedium  ThinkingLevel = "medium"
      ThinkingLevelHigh    ThinkingLevel = "high"
      ThinkingLevelMax     ThinkingLevel = "max"
  )

  func (l ThinkingLevel) Valid() bool
  func ParseThinkingLevel(s string) (ThinkingLevel, error)
  ```
- **Validation**:
  - `go test -race ./models/...` passes.
  - `go build ./...` from the repo root succeeds (no consumer references
    `models` yet, so this is a no-op for the build, but it confirms the
    package compiles).
  - `go vet ./models/...` is clean.
- **Details**:
  1. Create `models/spec.go` with the `Spec` struct. Use the pointer /
     zero-value split from the Architectural Blueprint. Document each
     field's wire-meaning ("Window is the model's context window size in
     tokens; not a request budget").
  2. Move `ThinkingLevel`, the `ThinkingLevel*` constants, `Valid()`, and
     `ParseThinkingLevel` from `provider/provider.go` (lines 156–230
     approximately) into `models/thinking.go`. Update the doc comment
     to reflect the new location.
  3. Add `models/spec_test.go` with table-driven tests covering:
     - Zero-value `Spec` (all fields zero / nil).
     - `Spec` with a `Temperature` pointer (verify pointer identity is
       preserved through assignment).
     - `ThinkingLevel.Valid()` for all six constants + an invalid string.
     - `ParseThinkingLevel("")` returns an error; `ParseThinkingLevel("high")`
       returns `ThinkingLevelHigh`.
  4. **Do not** update any existing call sites in this task. The
     `provider.ThinkingLevel` constants are temporarily duplicated; Task
     4 removes the duplicates in one atomic BC break.

### Task 2: Add `x/provider/openai/models/` subpackage with known specs

- **Goal**: Add a per-vendor subpackage exporting `Spec` constants for
  the OpenAI p80 case. No call sites change in this task; the constants
  exist for future use.
- **Dependencies**: Task 1.
- **Files Affected**: None.
- **New Files**:
  - `x/provider/openai/models/doc.go` — package doc, import example
    showing the alias pattern.
  - `x/provider/openai/models/gpt4o.go` — `GPT4o`, `GPT4oMini`, `O1`,
    `O1Mini`, `O3`, `O3Mini` (or the current set; verify against the
    OpenAI model catalog at implementation time).
  - `x/provider/openai/models/gpt4o_test.go` — table-driven tests
    asserting each constant has a non-empty `Name`, a non-zero
    `Window`, and a valid `ThinkingLevel`.
- **Interfaces**: None new (the constants are of type `models.Spec`).
- **Validation**:
  - `go test -race ./x/provider/openai/models/...` passes.
  - `go build ./...` succeeds.
  - The constants' `Name` field matches the actual OpenAI model name
    strings (verified by the implementer against the upstream API docs
    or a test that calls the OpenAI `/v1/models` endpoint and asserts
    the names exist).
- **Details**:
  1. Use the values from the OpenAI documentation for `Window` and
     `MaxOutputTokens`. Where a model's actual limits are not
     authoritatively documented, prefer the *lower* of plausible values
     (the framework is wrong-but-safe; the user can override with ad-hoc
     construction).
  2. Default `ThinkingLevel` to `ThinkingLevelOff` for non-reasoning
     models; set to `ThinkingLevelMedium` for `O1`, `O3`, etc. The
     default can be overridden per-call via the `Spec.ThinkingLevel`
     field at the call site.
  3. Do not include a `Registry()` function or `All()` accessor in v1 —
     the named constants are the public surface. (See Architectural
     Blueprint.)
  4. Add a doc comment in `doc.go` showing the recommended import alias
     pattern: `import openaimodels "github.com/.../ore/x/provider/openai/models"`.

### Task 3: Add `x/provider/anthropic/models/` subpackage with known specs

- **Goal**: Same as Task 2, for the Anthropic provider. Ships
  `ClaudeOpus45`, `ClaudeSonnet45`, `ClaudeHaiku45` (or the current set;
  verify against the Anthropic model catalog at implementation time).
- **Dependencies**: Task 1.
- **Files Affected**: None.
- **New Files**:
  - `x/provider/anthropic/models/doc.go`
  - `x/provider/anthropic/models/claude.go` (or split per model).
  - `x/provider/anthropic/models/claude_test.go`.
- **Interfaces**: None new.
- **Validation**:
  - `go test -race ./x/provider/anthropic/models/...` passes.
  - `go build ./...` succeeds.
- **Details**:
  1. Mirror the structure of Task 2. Verify `Window` and
     `MaxOutputTokens` against the Anthropic documentation. Anthropic
     models have a 200_000-token context window as of mid-2025; verify
     the current values at implementation time.
  2. The Anthropic provider's `WithCacheControl` option is not part of
     the `Spec`; it remains a per-call `InvokeOption` because it
     controls request-shape, not model identity. (Document this
     distinction in the spec doc.)
  3. For reasoning models (Opus 4.5 with extended thinking), set
     `ThinkingLevel` to `ThinkingLevelMedium` as a sensible default.

### Task 4: Refactor `Provider.Invoke` to take `models.Spec`; remove old `WithModel` / `ModelOption`

- **Goal**: Atomically change `Provider.Invoke` to accept `Spec` as a
  first argument. Remove `provider.WithModel(string)`,
  `provider.ModelOption`, and each provider's `WithModel(string)`
  constructor option. Update all call sites in the adapters and their
  tests. The repo builds and all tests pass after this task.
- **Dependencies**: Tasks 1, 2, 3.
- **Files Affected**:
  - `provider/provider.go` — remove `WithModel` (lines ~94–110),
    `ModelOption` (lines ~76–92), `MaxTokensOption` is unchanged.
    Update the `Provider` interface comment. (No code change in
    `MaxTokensOption`.)
  - `x/provider/openai/openai.go` — change `Invoke` signature;
    replace the per-call `modelName` type assertion with direct
    reads from `spec`; remove `WithModel(string)` constructor option
    (lines ~215–219).
  - `x/provider/openai/openai_test.go` — update all `Invoke` call
    sites to pass a `models.Spec{...}`. Add a test that the
    per-call spec fields (Temperature, ThinkingLevel, MaxOutputTokens)
    are translated to the wire format.
  - `x/provider/anthropic/anthropic.go` — same as OpenAI.
  - `x/provider/anthropic/anthropic_test.go` — same as OpenAI.
- **New Files**: None.
- **Interfaces**:
  ```go
  // provider/provider.go
  type Provider interface {
      Invoke(ctx context.Context, s state.State, spec models.Spec,
          ch chan<- artifact.Artifact, opts ...InvokeOption) error
  }

  // (WithModel, ModelOption — removed.)
  // (ToolsOption, MaxTokensOption, InvokeOption — unchanged.)
  ```
  The OpenAI and Anthropic constructors lose `WithModel(string)`;
  the model identity is supplied per-call via the `Spec.Name` field.
- **Validation**:
  - `go test -race ./provider/...` passes.
  - `go test -race ./x/provider/openai/...` passes.
  - `go test -race ./x/provider/anthropic/...` passes.
  - `go build ./...` succeeds for the entire repo (the
    `examples/`, `cmd/`, `x/compaction/`, and other call sites that
    pass a `provider.ModelOption` are *temporarily* broken; the
    follow-up tasks fix them).
  - **No new test passes are deferred to later tasks.** The provider
    tests must be green before this task is complete, even if the
    downstream code (loop, session, examples) is broken. The
    implementer should fix the broken downstream call sites as part
    of this task, *or* the task boundary is moved earlier. (See
    task-splitting note below.)
- **Details**:
  1. **Task splitting note**: this task is the biggest single BC break
     in the plan. If the implementer finds the surface too large for
     one reviewable PR, split it into two: (a) add the new
     `Invoke` signature alongside the old one (`Invoke2` or a
     `spec`-suffixed variant), deprecate the old; (b) migrate all
     callers; (c) remove the old signature. The plan as written
     assumes an atomic break, consistent with AGENTS.md. The
     implementer's call.
  2. In `provider/provider.go`, delete the `WithModel` and
     `ModelOption` blocks. Update the `Provider` interface comment
     to reference `models.Spec`.
  3. In `x/provider/openai/openai.go`:
     - Delete the `WithModel` constructor option.
     - Change the `Invoke` signature.
     - Remove the per-call `modelName` variable and the
       `provider.ModelOption` type assertion.
     - Read `spec.Name`, `spec.Temperature`, `spec.ThinkingLevel`,
       `spec.MaxOutputTokens`, `spec.StopSequences` directly from
       the `spec` argument.
     - Update the OTel span attribute to use `spec.Name` (with
       fallback to `p.model` if `spec.Name == ""` — this preserves
       a sensible default if a caller passes a zero-value `Spec`).
     - In `serializeMessages` and any other call site that reads
       `p.model`, replace with the spec argument.
  4. Mirror the changes in `x/provider/anthropic/anthropic.go`.
     Note that Anthropic's wire format has different field names for
     temperature (`Temperature` vs `temperature` — Go's JSON
     tags handle this) and thinking (`thinking` block vs
     `reasoning_effort` string).
  5. In each adapter's `_test.go`, find every `Invoke` call and
     add a `models.Spec{Name: "..."}` argument. Most existing tests
     use a hardcoded `WithModel` at construction; convert them to
     pass the spec per call.
  6. After this task, `provider.WithModel` and
     `provider.ModelOption` no longer exist; any code in the repo
     that referenced them is broken. **The follow-up tasks (5, 6,
     7, 8) fix the broken call sites as their primary work.** The
     validation criteria for this task require the *provider*
     tests to be green, not the full repo. The repo build
     (`go build ./...`) may be broken until Task 5 lands; the
     implementer can choose to fix call sites in this task if
     desired, or stage them in Task 5.

### Task 5: Refactor `loop.Step.Turn` and `loop.Pipeline.Turn` to take `models.Spec`

- **Goal**: Update the loop's `Turn` signature to take a `Spec`
  argument. Add a `WithDefaultSpec` option for pre-binding a default.
  The pipeline signature mirrors the step. The `Step` interface in
  `loop/interfaces.go` updates. All loop tests and any code that
  constructs a `Step` is updated.
- **Dependencies**: Task 4.
- **Files Affected**:
  - `loop/loop.go` — change `Step.Turn` signature; add
    `WithDefaultSpec` option. Update `Turn` to use the per-call
    spec or fall back to the configured default.
  - `loop/interfaces.go` — update `TurnRunner` interface.
  - `loop/pipeline.go` — change `Pipeline.Turn` signature.
  - `loop/loop_test.go` — update test call sites.
  - `loop/pipeline_test.go` — update test call sites.
- **New Files**: None.
- **Interfaces**:
  ```go
  // loop/loop.go
  type Step struct {
      // ... existing fields ...
      defaultSpec models.Spec
  }

  func WithDefaultSpec(spec models.Spec) Option { ... }

  func (s *Step) Turn(ctx context.Context, st state.State,
      spec models.Spec, p provider.Provider,
      opts ...provider.InvokeOption) (state.State, error)

  // loop/interfaces.go
  type TurnRunner interface {
      Turn(ctx context.Context, st state.State, spec models.Spec,
          p provider.Provider, opts ...provider.InvokeOption) (state.State, error)
  }

  // loop/pipeline.go
  func (p *Pipeline) Turn(ctx context.Context, st state.State,
      spec models.Spec, prov provider.Provider,
      onArtifact func(artifact.Artifact),
      opts ...provider.InvokeOption) (state.State, []artifact.Artifact, error)
  ```
- **Validation**:
  - `go test -race ./loop/...` passes.
  - `go build ./loop/...` succeeds.
  - The full `go build ./...` is *still* broken (the session and
    compaction packages call the old `Step.Turn`); this is fixed
    in Tasks 6 and 7.
- **Details**:
  1. Add `defaultSpec models.Spec` to the `Step` struct and the
     `WithDefaultSpec(spec models.Spec)` option.
  2. Change `Step.Turn`'s signature. Inside, resolve the effective
     spec: per-call wins over default.
  3. The pipeline `Turn` takes the spec as a parameter and passes
     it through to `prov.Invoke(ctx, st, spec, provCh, allOpts...)`.
  4. Update `TurnRunner` in `loop/interfaces.go` to match.
  5. Update all test call sites in `loop/loop_test.go` and
     `loop/pipeline_test.go`.

### Task 6: Refactor `session.Stream` to derive `Spec` from metadata

- **Goal**: Replace `Stream.ModelOption() provider.InvokeOption` with
  `Stream.Spec() (models.Spec, bool)`. The session reads
  `Thread.Metadata` and constructs a `Spec` (or returns `false` for
  "use the loop's default"). The slash command in
  `x/tool/set_model/setmodel.go` updates to write the new metadata
  keys.
- **Dependencies**: Task 5.
- **Files Affected**:
  - `session/stream.go` — remove `ModelOption()`; add `Spec()` and
    a private `deriveSpec()` helper.
  - `x/tool/set_model/setmodel.go` — update the metadata keys
    (private to the session package) and the `SetMetadata` calls.
  - `session/stream_test.go` — update tests.
  - `x/tool/set_model/setmodel_test.go` — update tests.
- **New Files**: None.
- **Interfaces**:
  ```go
  // session/stream.go
  // Spec returns the effective ModelSpec for the next turn, derived
  // from the thread's metadata. The bool result is false when the
  // metadata is absent or empty; the caller should use the loop's
  // default in that case. The framework does not merge session
  // metadata into a base Spec; the session is the only deriver.
  //
  // The slash command in x/tool/set_model writes the metadata
  // consumed by deriveSpec. The metadata keys are private to this
  // package; x/tool/set_model declares the same keys locally.
  func (s *Stream) Spec() (models.Spec, bool)

  // deriveSpec reads Thread.Metadata and constructs a Spec.
  // Recognized keys (private to this package):
  //   "ore.model.name"             → Spec.Name
  //   "ore.model.thinking_level"   → Spec.ThinkingLevel
  //   "ore.model.temperature"      → Spec.Temperature (parsed float)
  // (Additional keys may be added in follow-up issues; the prefix
  // "ore.model." reserves the namespace for framework-level model
  // configuration.)
  func (s *Stream) deriveSpec() (models.Spec, bool)

  // ModelOption is removed.
  ```
- **Validation**:
  - `go test -race ./session/...` passes.
  - `go test -race ./x/tool/set_model/...` passes.
  - `go build ./...` succeeds *if* Tasks 4 and 5 are also complete
    (the build chain is: `examples/...` → `session/...` → `loop/...`
    → `provider/...` → `models/...`; if any link is broken, the
    build fails).
- **Details**:
  1. The metadata key namespace: `ore.model.<field>`. Each recognized
     key maps to a `Spec` field. Unknown keys are ignored (forward
     compatibility for future fields).
  2. `deriveSpec` returns `(zero, false)` if no recognized keys are
     set. Returning `(spec, true)` means "use this spec instead of
     the loop's default."
  3. The `/model <name>` slash command continues to set the
     "name" key. Additional slash commands (e.g. `/thinking high`,
     `/temperature 0.7`) are out of scope for this plan but can be
     added later by writing the corresponding metadata keys.
  4. Update `x/tool/set_model/setmodel.go` to write
     `"ore.model.name"` instead of `"provider.model"`. The
     `metadataKey` constant moves to the session package; the slash
     command imports it.

### Task 7: Refactor `x/compaction/` to use `models.Spec`

- **Goal**: The compactor and its strategies take a `models.Spec` for
  both inference (the model used for summarization) and triggering
  (the window against which `TotalTokens` is compared). The
  `SummarizeStrategy` gains a `Spec` field; the `MaxTokens int64`
  field is folded into `Spec.MaxOutputTokens`. The
  `TokenUsageTrigger` reads `Spec.Window` instead of accepting a
  `MaxTokens int`.
- **Dependencies**: Task 5 (and Task 4 for the provider's new
  `Invoke` signature).
- **Files Affected**:
  - `x/compaction/compaction.go` — `TokenUsageTrigger` reads from
    `Spec.Window`.
  - `x/compaction/summarize.go` — `SummarizeStrategy` gains a
    `Spec` field; `MaxTokens` is removed (fold into `Spec.MaxOutputTokens`).
  - `x/compaction/compaction_test.go`, `summarize_test.go` —
    updated to construct `Spec` values.
  - `x/compaction/doc.go` — update the example to use `Spec`.
- **New Files**: None.
- **Interfaces**:
  ```go
  // x/compaction/compaction.go
  type TokenUsageTrigger struct {
      // Spec is the compactor's own model. The trigger compares
      // Usage.TotalTokens against Spec.Window. When Spec.Window
      // is zero, the trigger never fires (the compactor is
      // effectively disabled for window-based compaction).
      Spec models.Spec
  }

  func (t TokenUsageTrigger) ShouldCompact(turns []state.Turn) bool {
      // ... read t.Spec.Window ...
  }

  // x/compaction/summarize.go
  type SummarizeStrategy struct {
      Provider provider.Provider
      Spec     models.Spec  // independently configured; typically a
                            // different (cheaper) model than the
                            // main loop
      Prompt   string
      // MaxTokens is removed; use Spec.MaxOutputTokens.
  }

  func (s SummarizeStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error) {
      // ... pass s.Spec to s.Provider.Invoke ...
  }
  ```
- **Validation**:
  - `go test -race ./x/compaction/...` passes.
  - `go build ./...` succeeds.
- **Details**:
  1. The compactor's `Spec` is the *first* instance of the
     sub-agent pattern: a different task (summarization) uses a
     different (typically cheaper) model. The plan documents this
     in the `x/compaction` package doc.
  2. `TokenUsageTrigger.Spec.Window == 0` is a sensible "trigger
     disabled" state; the existing `len(turns) > 0` "no Usage
     found" graceful-degradation behavior is unchanged.
  3. The `SummarizeStrategy.MaxTokens` field is removed; callers
     that need a non-default output budget set
     `Spec.MaxOutputTokens` directly on the compactor's spec.
  4. Update `x/compaction/doc.go`'s example to use
     `Spec.MaxOutputTokens` instead of `MaxTokens: 8192` and
     `TokenUsageTrigger{Spec: openaimodels.GPT4oMini}` instead of
     `TokenUsageTrigger{MaxTokens: 8000}`.

### Task 8: Update examples and integration tests

- **Goal**: All example applications compile, build, and pass their
  integration tests after the refactor. Any in-line `WithModel` or
  `ModelOption` use is replaced with `Spec` construction.
- **Dependencies**: Tasks 4, 5, 6, 7.
- **Files Affected** (this list is best-effort; the implementer
  should grep the repo for `WithModel` and `ModelOption` to find
  all call sites):
  - `examples/tui-chat/main.go`
  - `examples/http-chat/main.go`
  - `examples/single-turn-cli/main.go`
  - `cmd/*/main.go` (if any)
  - `x/compaction/doc.go` (example block)
  - Any integration test that constructs a `Provider` and
    `Step` and calls `Turn`.
- **New Files**: None.
- **Validation**:
  - `go build ./...` succeeds for the entire repo.
  - `go test -race ./...` passes for the entire repo.
  - `examples/tui-chat` runs and exits cleanly (smoke test).
  - `examples/http-chat` starts and serves a `/send_message`
    request (smoke test).
- **Details**:
  1. In each example, replace
     `openai.New(openai.WithAPIKey(...), openai.WithModel("gpt-4o"))`
     with `openai.New(openai.WithAPIKey(...))` and pass
     `openaimodels.GPT4o` per-call (or via `WithDefaultSpec`).
  2. Remove any `stream.ModelOption()` calls; replace with
     `stream.Spec()` if the example uses the metadata-based
     override path, or remove the call entirely if the example
     didn't actually need the override.
  3. Update integration tests that mock the `Provider` to match
     the new `Invoke` signature.

### Task 9: Final verification, cleanup, and documentation

- **Goal**: Run the full test suite with race detection; remove
  any leftover `WithModel` / `ModelOption` references; update
  `AGENTS.md` if the new `models` package should be listed among
  the core primitives; close or update related issues.
- **Dependencies**: Tasks 1–8.
- **Files Affected**:
  - `AGENTS.md` — add `models/` to the list of core packages if
    appropriate.
  - `README.md` — add a brief mention if the `Spec` type is a
    user-facing primitive.
- **New Files**: None.
- **Validation**:
  - `go test -race ./...` passes for the entire repo.
  - `go vet ./...` is clean.
  - `grep -r "WithModel\|ModelOption" --include="*.go"` returns
    no results in production code (tests may still reference the
    old names for documentation purposes, but production code is
    clean).
  - `go.work` and `go.mod` are unchanged (no new sub-modules; the
    `models` package is in the root module).
- **Details**:
  1. Run `go test -race ./...` from the repo root. Fix any
     failures.
  2. Run `go vet ./...` and fix any warnings.
  3. Grep for `WithModel` and `ModelOption` to confirm no
     stale references.
  4. Update `AGENTS.md`'s "Package Structure" section to list
     `models/` alongside `state/`, `provider/`, `loop/`, etc.
  5. Comment on (do not auto-close) #445 noting that the
     "Model Defaults" intent is now expressed as default
     `ModelSpec` values in the per-vendor catalogs.
  6. Add a brief note to the changelog / release notes
     documenting the BC break: `WithModel(string)` and
     `ModelOption` are removed; `Spec` is the new
     model-identity primitive.

## Dependency Graph

```
Task 1 (create models package)
  ├── Task 2 (openai/models subpackage)
  │     └── Task 4 (Provider.Invoke refactor)
  │           ├── Task 5 (loop.Step.Turn refactor)
  │           │     ├── Task 6 (session.Stream.Spec)
  │           │     │     └── Task 8 (examples + integration)
  │           │     └── Task 7 (compaction refactor)
  │           │           └── Task 8
  │           └── Task 8
  └── Task 3 (anthropic/models subpackage)
        └── Task 4
              └── Task 9 (final verification)
```

Critical path: Task 1 → Task 2/3 (parallel) → Task 4 → Task 5 →
Task 6 || Task 7 → Task 8 → Task 9.

Parallelizable:
- Tasks 2 and 3 (different vendors) are independent.
- Tasks 6 and 7 (session vs compaction) are independent.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `Provider.Invoke` BC break ripples through more files than anticipated (e.g. third-party adapters not in this repo). | High | Med | The break is the user's explicit choice. AGENTS.md calls for aggressive refactoring. The `models.Spec` is a strict superset of what `WithModel` provided, so external adapters can be migrated by adding a Spec argument and reading the `Name` field. |
| `models` package ends up depending on `provider` (cycle risk). | High | Low | `models` is a leaf. `ThinkingLevel` moves to `models`; `provider` imports `models` for the type. The cycle is broken by the move, not by introducing a third package. |
| Pointer-typed fields on `Spec` cause nil-deref bugs in adapters that forget the nil check. | Med | High | Document each pointer field's "not set" semantic. Adapter code that reads a pointer field checks for nil and omits the wire field. Add a `TestProvider_OmitsZeroValueSpec` test for each adapter. |
| Per-vendor model catalogs ship stale `Window` values. | Med | Med | Document each constant's source (date and documentation URL). Add a CI check that fails if a constant's `Window` is older than 90 days without an update PR. (Out of scope for v1; track as a follow-up.) |
| The session's `Spec()` returns a `Spec` with only some fields set (e.g. `Name` set, `Temperature` not), and the loop's default has the opposite (e.g. `Temperature` set, `Name` not). The "per-call wins entirely" semantic means the loop's `Temperature` is dropped. | Med | Med | Document this explicitly: the per-call `Spec` *replaces* the default; the framework does not merge. If merging is needed later, the implementer can add a `loop.WithSpecOverrides(spec)` option. The current design is simpler. |
| `compaction.TokenUsageTrigger.Spec.Window == 0` is silently interpreted as "disabled" but the implementer might want a panic instead. | Low | Low | Document the zero-Window semantic. Add a test that asserts the trigger does not fire when `Spec.Window == 0`. |
| The plan's atomic BC break in Task 4 is too large for one reviewable PR. | Med | High | Per the Architectural Blueprint, the implementer can split Task 4 into (a) add new `Invoke` signature alongside old; (b) migrate callers; (c) remove old. The plan as written assumes atomic, consistent with AGENTS.md. |
| Example applications in `examples/` diverge from the new API in a way the implementer misses. | Low | High | Task 8 has an explicit "grep for `WithModel` / `ModelOption`" step. The full `go build ./...` is the canary. |
| `loop.Step` interface in `loop/interfaces.go` (the `TurnRunner` interface) is consumed by code outside this repo (cognitive patterns, examples). | Med | Med | The interface is updated atomically with the implementation. The blast radius is bounded by the same BC break; cognitive patterns and examples are part of this repo and updated in Task 8. |
| Adapters (OpenAI, Anthropic) that ignore certain `Spec` fields silently produce wrong output. | Med | Med | Document the "permissive Spec" semantic in `models.Spec`'s godoc. The Spec is intentionally a flat struct; adapters are responsible for translating the fields they understand. Add per-adapter tests that assert each Spec field is translated to the wire format. |

## Validation Criteria

- [ ] `go test -race ./...` passes for the entire repository.
- [ ] `go vet ./...` is clean.
- [ ] `go build ./...` succeeds for the entire repository.
- [ ] `models.ModelSpec` exists at the root level with the documented
      fields.
- [ ] `models.ThinkingLevel` and the `ThinkingLevel*` constants exist
      in the `models` package; `provider.ThinkingLevel` and
      `provider.ThinkingLevel*` are removed.
- [ ] `provider.WithModel(string)` and `provider.ModelOption` are
      removed; no references in production code.
- [ ] `Provider.Invoke` signature is
      `Invoke(ctx, state, spec models.Spec, ch, opts...)`.
- [ ] Both `x/provider/openai/openai.go` and
      `x/provider/anthropic/anthropic.go` translate `Spec.Temperature`,
      `Spec.ThinkingLevel`, `Spec.MaxOutputTokens`,
      `Spec.StopSequences` to the wire format.
- [ ] `x/provider/openai/models/` and `x/provider/anthropic/models/`
      subpackages exist and export at least three named `Spec`
      constants each.
- [ ] `loop.Step.Turn` signature accepts a `Spec` argument.
- [ ] `loop.Step` has a `WithDefaultSpec` option.
- [ ] `loop.Pipeline.Turn` signature mirrors `Step.Turn`.
- [ ] `session.Stream.Spec() (models.Spec, bool)` exists; the old
      `Stream.ModelOption()` is removed.
- [ ] `x/tool/set_model/setmodel.go` writes the new metadata keys
      consumed by `Stream.Spec()`.
- [ ] `compaction.TokenUsageTrigger` reads `Spec.Window`; the old
      `MaxTokens int` field is removed.
- [ ] `compaction.SummarizeStrategy` has a `Spec` field; the old
      `MaxTokens int64` field is removed.
- [ ] All `examples/*` and `cmd/*` binaries compile and pass smoke
      tests.
- [ ] `grep -r "WithModel\b" --include="*.go" .` returns no results
      in production code.
- [ ] `grep -r "ModelOption\b" --include="*.go" .` returns no
      results in production code.
- [ ] `AGENTS.md` documents the `models/` core package.

## Future Work (out of scope for this plan)

### Compactor as a sub-agent

The compactor is conceptually a sub-agent — its own model choice, its
own inference configuration, a different (simpler) task. Designing its
`ModelSpec` as an independently-configured value is the first concrete
instance of a more general sub-agent pattern. A future refactor should:

- Extract a small "sub-agent" primitive that the compactor is one
  instance of.
- Allow other sub-agents (classification, routing, summarization-with-different-prompts, …)
  to be expressed as `Spec`-bearing functions.
- Generalize the "different model for different tasks" pattern to a
  registry or routing layer.

Tracked as a follow-up issue; not in scope here.

### Capabilities as a `Spec` method

When a concrete use case emerges (e.g. a TUI status indicator showing
"this model supports image input"), add a `Capabilities()` method to
`Spec`. The Spec is the natural place; the design is deferred until a
consumer exists.

### Per-vendor model registry

A `openaimodels.All() []models.Spec` and
`anthropicmodels.All() []models.Spec` registry function. Useful for
tooling (a `/models` slash command, a setup wizard, a TUI model picker).
Out of scope for v1; the named constants are the primary surface.
