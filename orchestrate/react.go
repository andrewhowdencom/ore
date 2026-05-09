package orchestrate

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/loop"
	"github.com/andrewhowdencom/tack/surface"
)

// ReAct is an Orchestrator that implements the ReAct pattern: it loops on
// Step execution while tool results are appended to state, driving the
// assistant to reason, act, and observe until no more tool calls remain.
type ReAct struct {
	State    state.State
	Step     *loop.Step
	Surface  surface.Surface
	Provider provider.Provider

	mu         sync.Mutex
	cancelFunc context.CancelFunc
}

// Compile-time interface check.
var _ Orchestrator = (*ReAct)(nil)

// Run starts the ReAct event loop, reading events from the surface and
// processing user messages in a sequential worker goroutine. It blocks until
// the context is cancelled or the surface's event channel is closed.
func (r *ReAct) Run(ctx context.Context) error {
	eventsCh := r.Surface.Events()

	// Buffered channel ensures at most one pending event.
	workCh := make(chan surface.Event, 1)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for event := range workCh {
			opCtx, cancel := context.WithCancel(ctx)
			r.mu.Lock()
			r.cancelFunc = cancel
			r.mu.Unlock()

			switch e := event.(type) {
			case surface.UserMessageEvent:
				if err := r.handleUserMessage(opCtx, e); err != nil {
					slog.Error("handle user message failed", "err", err)
				}
			case surface.InterruptEvent:
				// Nothing to do — interrupt is handled by cancelling opCtx.
			}

			r.mu.Lock()
			r.cancelFunc = nil
			r.mu.Unlock()
			cancel()
		}
	}()

	defer func() {
		close(workCh)
		wg.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-eventsCh:
			if !ok {
				return nil
			}
			switch e := event.(type) {
			case surface.UserMessageEvent:
				select {
				case workCh <- e:
				case <-ctx.Done():
					return ctx.Err()
				}
			case surface.InterruptEvent:
				r.mu.Lock()
				if r.cancelFunc != nil {
					r.cancelFunc()
				}
				r.mu.Unlock()
			}
		}
	}
}

func (r *ReAct) handleUserMessage(ctx context.Context, event surface.UserMessageEvent) error {
	r.State.Append(state.RoleUser, artifact.Text{Content: event.Content})

	for {
		if err := r.Surface.SetStatus(ctx, "thinking..."); err != nil {
			slog.Error("set status failed", "err", err)
		}

		before := len(r.State.Turns())
		result, err := r.Step.Turn(ctx, r.State, r.Provider)
		if err != nil {
			_ = r.Surface.SetStatus(ctx, fmt.Sprintf("error: %v", err))
			return err
		}

		after := len(result.Turns())
		for i := before; i < after; i++ {
			if err := r.Surface.RenderTurn(ctx, result.Turns()[i]); err != nil {
				slog.Error("render turn failed", "err", err)
			}
		}

		turns := result.Turns()
		if len(turns) == 0 {
			_ = r.Surface.SetStatus(ctx, "")
			return nil
		}

		last := turns[len(turns)-1]
		if last.Role == state.RoleAssistant {
			_ = r.Surface.SetStatus(ctx, "")
			return nil
		}

		// Tool results were appended; need another assistant turn.
		if err := r.Surface.SetStatus(ctx, "calling tool..."); err != nil {
			slog.Error("set status failed", "err", err)
		}
	}
}
