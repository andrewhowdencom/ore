package loop

import (
	"context"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
)

// Pipeline is the single-turn execution engine. It runs transforms, invokes
// the provider, accumulates streaming artifacts (with delta merging), and
// executes registered handlers.
type Pipeline struct {
	transforms []Transform
	handlers   []Handler
	invokeOpts []provider.InvokeOption
}

// newPipeline creates a Pipeline with default values.
func newPipeline() *Pipeline {
	return &Pipeline{}
}

// Turn performs one inference turn: runs transforms, calls the provider,
// accumulates artifacts, and invokes onArtifact for each artifact
// (including deltas and flushed accumulated blocks). Returns the final
// accumulated artifacts and any error.
//
// The spec carries the model identity and inference configuration;
// it is forwarded to the provider's Invoke method.
func (p *Pipeline) Turn(ctx context.Context, st state.State, spec models.Spec, prov provider.Provider, onArtifact func(artifact.Artifact), opts ...provider.InvokeOption) (state.State, []artifact.Artifact, error) {
	var err error

	for _, tr := range p.transforms {
		st, err = tr.Transform(ctx, st)
		if err != nil {
			return st, nil, fmt.Errorf("transform failed: %w", err)
		}
	}

	provCh := make(chan artifact.Artifact, 100)
	var accumulatedArtifacts []artifact.Artifact

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		accumulators := make(map[string]artifact.Artifact)
		var keys []string

		for art := range provCh {
			if d, ok := art.(artifact.Accumulable); ok {
				key := d.AccumulatorKey()
				if _, exists := accumulators[key]; !exists {
					keys = append(keys, key)
				}
				accumulators[key] = d.MergeInto(accumulators[key])
				onArtifact(art)
				if ctx.Err() != nil {
					return
				}
			} else {
				// Flush all accumulated artifacts before handling non-delta.
				for _, key := range keys {
					acc := accumulators[key]
					onArtifact(acc)
					accumulatedArtifacts = append(accumulatedArtifacts, acc)
				}
				accumulators = make(map[string]artifact.Artifact)
				keys = nil

				onArtifact(art)
				if ctx.Err() != nil {
					return
				}
				accumulatedArtifacts = append(accumulatedArtifacts, art)
			}
		}

		// Flush remaining accumulated artifacts at stream end.
		for _, key := range keys {
			acc := accumulators[key]
			onArtifact(acc)
			accumulatedArtifacts = append(accumulatedArtifacts, acc)
		}
	}()

	allOpts := make([]provider.InvokeOption, 0, len(p.invokeOpts)+len(opts))
	allOpts = append(allOpts, p.invokeOpts...)
	allOpts = append(allOpts, opts...)

	err = prov.Invoke(ctx, st, spec, provCh, allOpts...)
	close(provCh)
	wg.Wait()

	// Post-accumulation: attach display hints to ToolCall artifacts.
	applyDisplayHints(ctx, accumulatedArtifacts, allOpts)

	if err != nil {
		return st, accumulatedArtifacts, err
	}

	return st, accumulatedArtifacts, nil
}

// RunHandlers executes all registered handlers on each artifact,
// using the provided Emitter for any handler-side emissions.
func (p *Pipeline) RunHandlers(ctx context.Context, artifacts []artifact.Artifact, emitter Emitter) error {
	for _, art := range artifacts {
		for _, h := range p.handlers {
			if err := h.Handle(ctx, art, emitter); err != nil {
				return fmt.Errorf("artifact handler failed: %w", err)
			}
		}
	}
	return nil
}
