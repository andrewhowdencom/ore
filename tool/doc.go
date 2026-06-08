// Package tool defines the core tool execution framework for ore.
//
// It provides the universal contracts for tool registration (Registry),
// tool functions (ToolFunc), remote tool sources (RemoteSource), and
// schema validation (ValidateSchema).
//
// Sandboxes
//
// Sandbox interfaces enable per-tool-call isolation. The handler resolves a
// sandbox for each tool call and passes it to the ToolFunc. Tools opt into
// available capabilities via type assertions on the received Sandbox.
//
//   - Sandbox — base interface with Name(). A nil Sandbox means no isolation;
//     tools execute against the host filesystem and process space.
//   - FileSandbox — extends Sandbox with ResolvePath(path) (string, error) and
//     WorkingDirectory() string. Tools type-assert to FileSandbox when they
//     need path resolution and working directory constraints.
//   - ExecSandbox — extends Sandbox with Run(ctx, cmd, dir, timeout) which
//     delegates command execution to the sandbox. Tools type-assert to
//     ExecSandbox when they need process isolation.
//
// SandboxRegistry (see registry.go) extends Registry with methods to register,
// look up, and set a default sandbox. Handlers type-assert the registry to
// SandboxRegistry to resolve sandboxes per tool call. If the registry does not
// implement SandboxRegistry, all tool calls receive a nil sandbox.
//
// Tool Output Budget
//
// The Tool struct exposes a MaxBytes field that defines a hard ceiling on the
// serialized JSON output size in bytes, and a TruncationHint template string
// that is appended when the limit is exceeded. MaxBytes is a framework-level
// safety rail: when a tool's JSON result exceeds the limit, the framework
// truncates the Content string to a valid UTF-8 boundary and appends the formatted
// hint, with the ${N} placeholder replaced by the actual total byte count.
// This prevents unbounded token consumption from tools that can return arbitrarily
// large output (e.g. recursive directory searches). Tools may also implement
// cooperative truncation by respecting the same budget during their own execution.
// The TruncateContent helper function is available for tools that want to measure
// and cap their own output before returning.
//
// Concrete tool implementations, discovery mechanisms, and the loop.Handler
// bridge live in the x/tool/ extension packages. This package defines only
// the contracts that core packages (cognitive/, session/, loop/) can import
// without creating dependency cycles.
//
// This separation is intentional: placing the handler bridge in x/tool/
// prevents the core contracts from importing loop/ or provider/, preserving
// the framework's cycle-free dependency graph.
//
// The default in-memory Registry implementation is analogous to state.Buffer:
// it is not goroutine-safe, and concurrency control is a future middleware
// concern.
package tool
