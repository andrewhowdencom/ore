package loop

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
)

// Emitter is the interface exposed to artifact handlers for emitting
// events back into the Step's output stream.
type Emitter interface {
	Emit(ctx context.Context, event OutputEvent)
}

// Handler processes individual artifacts from an assistant turn.
// Multiple handlers may be registered on a Step; each handler inspects the
// artifact Kind() and acts only on types it understands.
type Handler interface {
	// Handle processes a single artifact. It may emit events via the
	// provided Emitter (e.g., emit a TurnCompleteEvent with RoleTool and
	// tool results) or perform other side effects. Using Emitter instead
	// of mutating state directly keeps all observable mutations on the same
	// event stream, ensuring UI conduits receive tool results without
	// special-casing.
	Handle(ctx context.Context, art artifact.Artifact, e Emitter) error
}
