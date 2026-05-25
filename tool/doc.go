// Package tool defines the core tool execution framework for ore.
//
// It provides the universal contracts for tool registration (Registry),
// tool functions (ToolFunc), remote tool sources (RemoteSource), and
// schema validation (ValidateSchema).
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
