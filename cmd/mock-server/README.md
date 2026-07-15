# mock-server

A wire-compatible mock LLM server for the ore framework. Serves canned
responses in either the OpenAI chat completions or Anthropic messages
streaming wire format.

## Why

Application code (the workshop, perf probes, integration tests) needs
to exercise the framework end-to-end without paying real LLM latency.
Pointing a real `x/provider/openai` or `x/provider/anthropic` provider
at this server's bound URL lets you measure framework overhead —
serialization, state assembly, tool routing, artifact emission — in
isolation from upstream variability.

## Usage

Build:

```bash
go build -o mock-server ./cmd/mock-server
```

Run with a JSON config:

```bash
./mock-server -vendor=openai -config=responses.json
./mock-server -vendor=anthropic -config=responses.json -addr=:8080
```

On startup, the server prints a single line to stderr:

```
mock-server: listening on http://127.0.0.1:8080 (vendor=openai)
```

This is the contract callers use to discover the bound URL (including
the ephemeral port when `-addr=:0`). Pipe stderr through `grep` or
parse the line directly.

The server runs until SIGTERM or SIGINT.

## JSON config schema

The config is an array of `mock.Response` values. The server rotates
through the array per HTTP request, wrapping on overflow. A
single-element array collapses to "same response forever".

```go
type Response struct {
    Text       string     `json:"text,omitempty"`
    Reasoning  string     `json:"reasoning,omitempty"`
    Signature  string     `json:"signature,omitempty"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    StopReason string     `json:"stop_reason,omitempty"`
    Usage      *Usage     `json:"usage,omitempty"`
}

type ToolCall struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // raw JSON
}

type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

Example:

```json
[
    {
        "text": "Hello, world!",
        "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
    },
    {
        "reasoning": "Let me think about this...",
        "signature": "sig_abc",
        "text": "After consideration, the answer is 42.",
        "tool_calls": [
            {"id": "call_1", "name": "calculator", "arguments": "{\"expr\":\"6*7\"}"}
        ]
    }
]
```

## OpenAI example

```bash
cat > /tmp/openai.json <<'EOF'
[{"text":"hi from the mock"}]
EOF

./mock-server -vendor=openai -config=/tmp/openai.json -addr=:11434 &

curl -N http://127.0.0.1:11434/chat/completions \
    -H 'content-type: application/json' \
    -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}'
```

Output:

```
data: {"id":"chatcmpl-mock-1-...","object":"chat.completion.chunk",...,"choices":[{"index":0,"delta":{"content":"hi from the mock"}}]}

data: {"id":"chatcmpl-mock-1-...","object":"chat.completion.chunk",...,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

## Anthropic example

```bash
cat > /tmp/anthropic.json <<'EOF'
[{"text":"hi from the mock"}]
EOF

./mock-server -vendor=anthropic -config=/tmp/anthropic.json -addr=:11434 &

curl -N http://127.0.0.1:11434/v1/messages \
    -H 'content-type: application/json' \
    -H 'anthropic-version: 2023-06-01' \
    -d '{"model":"claude-3-7-sonnet-latest","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}'
```

Output:

```
event: message_start
data: {"type":"message_start","message":{...}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi from the mock"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{...}}

event: message_stop
data: {"type":"message_stop"}
```

## Programmatic use

For tests and embedded use, import the sub-modules directly:

```go
import (
    openaimock "github.com/andrewhowdencom/ore/x/provider/mock/openai"
    "github.com/andrewhowdencom/ore/x/provider/mock"
)

srv, _ := openaimock.New(openaimock.WithResponses(mock.Response{
    Text: "Hello!",
}))
ts := httptest.NewServer(srv.Handler())
defer ts.Close()

// Wire the real provider at the mock:
p, _ := openai.New(openai.WithBaseURL(ts.URL), openai.WithAPIKey("test"))
```

## Notes

- The mock is stdlib-only; no SDK dependencies in production code.
- All wire-format translation is hand-rolled with `encoding/json`.
- The mock has no concept of input-driven responses — every request
  pulls the next canned response from the queue regardless of input.
- Both `/v1/chat/completions` and `/chat/completions` paths are
  registered on the OpenAI mock; the Anthropic mock accepts
  `/v1/messages`. Callers don't need to reason about base-URL shape.
