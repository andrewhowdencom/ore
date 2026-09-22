// Package usage provides a loop.Handler that accumulates token usage metadata
// from artifact.Usage artifacts and emits PropertiesEvent with running session
// totals. It is designed to be wired into a loop.Step alongside other handlers.
//
// The handler is thread-safe (protected by sync.Mutex) and gracefully skips
// turns where no Usage artifact is present (e.g., when the provider stream is
// interrupted before the final usage chunk arrives). Handle never returns an
// error; non-Usage artifacts are silently ignored.
//
// A PropertiesEvent is emitted for every Usage artifact, even when all token
// counts are zero.
//
// The emitted PropertiesEvent contains the following string key/value pairs:
//
//   - "sent":        latest-request prompt token count
//   - "cache_read":  latest-request cached prompt tokens, omitted when zero
//   - "cache_write": latest-request prompt-cache writes, omitted when zero
//   - "received":    latest-request completion token count
//   - "thinking":    latest-request reasoning token count
//   - "total":       cumulative total token count over the handler's lifetime
//
// Cache accounting varies by provider. OpenAI includes cached prompt tokens in
// sent and total; other APIs may report cache buckets separately. Consumers
// should display the breakdown without deriving a replacement total from it.
//
// # Usage
//
//	import (
//	    "github.com/andrewhowdencom/ore/loop"
//	    "github.com/andrewhowdencom/ore/x/usage"
//	)
//
//	handler := usage.New()
//	step := loop.New(
//	    loop.WithHandlers(handler),
//	)
//
// # Deprecation Consideration
//
// x/telemetry provides per-artifact, per-role OpenTelemetry character metrics
// that offer finer-grained attribution than token-level PropertiesEvent.
// PropertiesEvent may be deprecated in future framework versions in favor
// of these metrics. However, PropertiesEvent is still required for TUI status
// bar rendering until the TUI is updated to consume telemetry metrics directly.
package usage
