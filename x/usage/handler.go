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
	mu          sync.Mutex
	prompt      int
	completion  int
	total       int
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
	h.prompt += u.PromptTokens
	h.completion += u.CompletionTokens
	h.total += u.TotalTokens
	prompt := h.prompt
	completion := h.completion
	total := h.total
	h.mu.Unlock()

	e.Emit(ctx, loop.PropertiesEvent{
		Properties: map[string]string{
			"sent":     strconv.Itoa(prompt),
			"received": strconv.Itoa(completion),
			"total":    strconv.Itoa(total),
		},
	})
	return nil
}
