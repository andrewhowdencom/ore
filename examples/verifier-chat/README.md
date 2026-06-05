# verifier-chat

A reference application demonstrating the `WithVerification` cognitive pattern.
The agent receives a coding task, writes Go code using filesystem tools, and
must pass quality gates (`go build`, `go test`, `gofmt`) before returning.
If any gate fails, the combined report is injected as a system turn and the
agent retries.

## Usage

```bash
export ORE_API_KEY=...
export ORE_MODEL=gpt-4o
go run ./examples/verifier-chat "Write a function that reverses a string"
```

The agent works in a temporary directory that is cleaned up on exit. The
example demonstrates how `ReAct` can be wrapped with `WithVerification` to
add exec-based quality gates to any cognitive pattern.
