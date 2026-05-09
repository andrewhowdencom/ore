package loop

import (
	"context"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/state"
)

// Handler processes individual artifacts from an assistant turn.
// Multiple handlers may be registered on a Step; each handler inspects the
// artifact Kind() and acts only on types it understands.
type Handler interface {
	// Handle processes a single artifact. It may mutate state (e.g., append
	// a RoleTool turn with tool results) or perform side effects.
	Handle(ctx context.Context, art artifact.Artifact, s state.State) error
}
