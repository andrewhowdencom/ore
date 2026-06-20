// Package usage provides a loop.Handler that aggregates token usage from
// artifact.Usage artifacts and broadcasts running totals via PropertiesEvent.
//
// It is designed to be wired as a handler on a loop.Step alongside other
// artifact handlers (e.g., tool execution). Usage artifacts are left in
// state so models can optionally see consumption history; this handler
// only aggregates and emits metadata.
//
// Example:
//
//	step := loop.New(loop.WithHandlers(usage.New(), otherHandler))
package usage

import (
	"context"
	"strconv"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
)

// Handler aggregates token counts from artifact.Usage artifacts and emits a
// PropertiesEvent after each one so conduits can display running totals.
type Handler struct {
	mu         sync.Mutex
	prompt     int
	completion int
	total      int
	// thinking holds the per-turn thinking-token count, or nil when the
	// provider did not report `output_tokens_details` (e.g., a proxy
	// that strips the field). When non-nil it is overwritten on every
	// turn — never accumulated — because thinking token counts are a
	// property of the most recent request, not the running total.
	thinking *int
}

// New creates a new usage aggregation handler.
func New() *Handler {
	return &Handler{}
}

// Compile-time interface check.
var _ loop.Handler = (*Handler)(nil)

// Handle inspects the artifact for artifact.Usage, adds its counts to the
// running totals, and emits a PropertiesEvent with the aggregated values.
// All other artifact kinds are ignored.
func (h *Handler) Handle(ctx context.Context, art artifact.Artifact, e loop.Emitter) error {
	u, ok := art.(artifact.Usage)
	if !ok {
		return nil
	}

	h.mu.Lock()
	// Update: prompt, completion, and thinking track the last turn's values
	// (current API request size), not cumulative. Total remains cumulative
	// for billing tracking.
	h.prompt = u.PromptTokens
	h.completion = u.CompletionTokens
	h.thinking = u.ThinkingTokens
	h.total += u.TotalTokens
	prompt := h.prompt
	completion := h.completion
	thinking := h.thinking
	total := h.total
	h.mu.Unlock()

	e.Emit(ctx, loop.PropertiesEvent{
		Properties: map[string]string{
			"sent":     strconv.Itoa(prompt),
			"received": strconv.Itoa(completion),
			"thinking": thinkingString(thinking),
			"total":    strconv.Itoa(total),
		},
	})
	return nil
}

// thinkingString renders the per-turn thinking-token count for the TUI
// status bar. The three states map to three renderings:
//
//   - nil  -> "?"  (provider did not report; TUI shows Ψ ?)
//   - &0   -> "0"  (provider reported zero; TUI shows Ψ 0)
//   - &N   -> N    (provider reported N;    TUI shows Ψ N)
//
// Centralising the conversion here keeps the TUI free of pointer
// arithmetic and lets the contract evolve in one place.
func thinkingString(t *int) string {
	if t == nil {
		return "?"
	}
	return strconv.Itoa(*t)
}
