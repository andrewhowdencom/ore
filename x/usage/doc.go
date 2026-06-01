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
//   - "prompt_tokens":     cumulative prompt token count
//   - "completion_tokens": cumulative completion token count
//   - "total_tokens":      cumulative total token count
//
// # Usage
//
//	import (
//	    "github.com/andrewhowdencom/ore/loop"
//	    "github.com/andrewhowdencom/ore/x/usage"
//	)
//
//	handler := usage.NewHandler()
//	step := loop.New(
//	    loop.WithHandlers(handler),
//	)
package usage
