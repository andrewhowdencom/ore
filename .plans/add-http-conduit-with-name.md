# Plan: Add WithName Option to HTTP Conduit for Web UI Branding

## Objective

Add a `WithName(string)` functional option to the HTTP conduit (`x/conduit/http`) that allows applications to override the hardcoded "ore chat" branding in both the `<title>` and `<div id="header">` elements of the embedded web chat UI. This brings the HTTP conduit into parity with the TUI conduit, which already supports `WithName` for the terminal window title.

## Context

The HTTP conduit embeds a web chat UI via `x/conduit/http/static/index.html`, served by the `serveUI()` handler in `x/conduit/http/handler.go`. Two elements currently contain the hardcoded string "ore chat":

- `<title>ore chat</title>` (line 6)
- `<div id="header">ore chat</div>` (line 208)

The TUI conduit (`x/conduit/tui/tui.go`) already exports:

```go
func WithName(name string) Option {
    return func(t *TUI) {
        t.name = name
    }
}
```

with a default of `"Ore"` in `New()`. The HTTP conduit lacks any equivalent option.

The `static/index.html` file is embedded via `//go:embed static/*` and read at runtime through a package-level `fs.ReadFileFS` (`staticFS`), allowing test substitution via mock filesystems.

## Architectural Blueprint

The selected approach is to treat `static/index.html` as a Go `html/template`, replacing the hardcoded "ore chat" occurrences with `{{.Name}}` placeholders. The `serveUI()` method will read the embedded file, parse it as a template, execute it with the handler's configured name, and write the rendered HTML to the response.

**Rationale:**
- `html/template` is the idiomatic Go mechanism for injecting dynamic values into HTML. It provides automatic HTML escaping and is extensible for future branding variables.
- `strings.ReplaceAll` would be simpler but is fragile: any change to the literal string in the HTML would silently break the substitution.
- JavaScript-based replacement would keep the file static but is over-engineered for this use case and introduces a client-side dependency.

**Changes needed:**
1. Add `name string` field to `Handler` struct.
2. Add `WithName(name string) Option` following the TUI pattern.
3. Set default `name: "ore chat"` in `New()` to preserve current out-of-box behavior.
4. Modify `static/index.html` to use `{{.Name}}` in both `<title>` and `<div id="header">`.
5. Modify `serveUI()` to parse the file as `html/template`, execute with `struct{ Name string }`, and stream the result to `http.ResponseWriter`.
6. Add unit tests verifying the option is stored and that the rendered HTML contains the custom name.

## Requirements

1. A new `WithName(name string) Option` functional option on the HTTP conduit, matching the TUI conduit's API.
2. The option must replace both the `<title>` and `<div id="header">` text in the served `index.html`.
3. The default name when no option is provided must remain "ore chat" to preserve existing behavior.
4. Existing tests must continue to pass without modification (they assert "ore chat" is present in the default response).
5. New tests must verify that a custom name passed via `WithName` appears in the rendered HTML and that the default name is absent.

## Task Breakdown

### Task 1: Add WithName Option and Template HTML Serving
- **Goal**: Add `WithName` functional option, `name` field on `Handler`, and convert `serveUI()` to execute `static/index.html` as an `html/template`.
- **Dependencies**: None.
- **Files Affected**:
  - `x/conduit/http/handler.go` — add `name` field, `WithName` option, default in `New()`, update `serveUI()`
  - `x/conduit/http/static/index.html` — replace "ore chat" with `{{.Name}}`
- **New Files**: None.
- **Interfaces**:
  - New option: `func WithName(name string) Option`
  - Modified struct: `Handler` gains `name string` field
  - Modified method: `serveUI` now parses and executes `html/template`
- **Validation**:
  - `go test -race ./x/conduit/http/...` passes
  - `go build ./x/conduit/http/...` succeeds
- **Details**:
  1. In `handler.go`, add `name string` to the `Handler` struct.
  2. Add `WithName(name string) Option` with documentation mirroring the TUI's `WithName`.
  3. In `New()`, set default `name: "ore chat"` alongside existing defaults.
  4. Import `html/template` in `handler.go`.
  5. In `serveUI()` for the `/chat` path:
     - Read `static/index.html` from `staticFS` as before.
     - Parse the bytes as `template.New("index").Parse(string(data))`.
     - On parse error, return HTTP 500.
     - Execute the template with `struct{ Name string }{Name: h.name}`.
     - Set `Content-Type: text/html; charset=utf-8` and write the executed template directly to `w`.
  6. In `static/index.html`:
     - Change `<title>ore chat</title>` to `<title>{{.Name}}</title>`.
     - Change `<div id="header">ore chat</div>` to `<div id="header">{{.Name}}</div>`.

### Task 2: Add Unit Tests for WithName
- **Goal**: Verify the `WithName` option is stored correctly and that the rendered HTML reflects the custom name.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/conduit/http/handler_test.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./x/conduit/http/...` passes with all new assertions green
- **Details**:
  1. Add `TestNew_WithName(t *testing.T)` following the TUI test pattern:
     - Create a handler with `WithName("my-app")`.
     - Type-assert the returned `conduit.Conduit` to `*Handler`.
     - Assert `h.name == "my-app"`.
  2. In `TestHandler_WithUI_StaticFiles`, add a new sub-test:
     - Create a handler with `WithName("Custom App")`.
     - Request `GET /chat`.
     - Assert the response body contains `"Custom App"`.
     - Assert the response body does NOT contain `"ore chat"`.
  3. Confirm the existing `"GET /chat returns text/html"` sub-test still passes (it asserts `"ore chat"` is present, which remains the default).

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on Task 1 because it tests the new option and template rendering)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Template parse error due to malformed HTML in `static/index.html` | High | Low | Template is parsed from the same bytes previously served raw; any parse error would be caught immediately by `go test ./...`. The template syntax `{{.Name}}` is valid HTML. |
| `html/template` import introduces an unused import if implementation is wrong | Low | Low | `go vet` and compilation will catch unused imports immediately. |
| Existing tests fail because default "ore chat" is no longer present | Medium | Low | Default remains "ore chat"; existing assertions are preserved. Verified by running tests after Task 1. |
| Test mock `errorFS` returns error before template parsing, masking template error path | Low | Low | The `errorFS` test covers the `ReadFile` error path, which occurs before template parsing. A separate test for template parse errors is not needed because the embedded file is compile-time validated. |

## Validation Criteria

- [ ] `go test -race ./x/conduit/http/...` passes
- [ ] `go build ./x/conduit/http/...` succeeds
- [ ] `WithName("Custom App")` produces an HTTP response containing `<title>Custom App</title>` and `<div id="header">Custom App</div>`
- [ ] Handler created without `WithName` still produces `<title>ore chat</title>` and `<div id="header">ore chat</div>`
- [ ] The `TestNew_WithName` test asserts the `name` field is stored on the concrete `*Handler`
