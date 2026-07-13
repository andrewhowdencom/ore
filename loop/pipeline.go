package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
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
func (p *Pipeline) Turn(ctx context.Context, st ledger.State, spec models.Spec, prov provider.Provider, onArtifact func(artifact.Artifact), opts ...provider.InvokeOption) (ledger.State, []artifact.Artifact, error) {
	var err error

	for _, tr := range p.transforms {
		st, err = tr.Transform(ctx, st)
		if err != nil {
			return st, nil, fmt.Errorf("transform failed: %w", err)
		}
	}

	provCh := make(chan artifact.Artifact, 100)
	var accumulatedArtifacts []artifact.Artifact

	allOpts := make([]provider.InvokeOption, 0, len(p.invokeOpts)+len(opts))
	allOpts = append(allOpts, p.invokeOpts...)
	allOpts = append(allOpts, opts...)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		accumulators := make(map[string]artifact.Artifact)
		var keys []string

		for art := range provCh {
			// Apply display hints per-artifact so every consumer of the
			// ArtifactEvent stream (TUI, HTTP, exporters) sees a populated
			// Display, not the legacy raw-Arguments fallback. The post-pass
			// at the end of Turn remains as a safety net for any code path
			// that bypasses the pipeline; it is idempotent because hintArtifact
			// reapplies the same hint with the same input.
			art = hintArtifact(ctx, art, allOpts)
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

// hintForName returns the DisplayHint closure registered for the tool
// of the given name, or nil if no such tool is registered or the tool
// has no hint. The lookup walks every InvokeOption for a ToolsOption
// and returns the first matching tool's DisplayHint. It is shared
// between the per-artemit hint application in Pipeline.Turn and the
// post-pass applyDisplayHints; both call sites produce identical
// results because DisplayHint is a pure function.
func hintForName(ctx context.Context, name string, opts []provider.InvokeOption) func(map[string]any) any {
	for _, opt := range opts {
		to, ok := opt.(provider.ToolsOption)
		if !ok {
			continue
		}
		for _, t := range to.Tools(ctx, nil) {
			if t.Name == name && t.DisplayHint != nil {
				return t.DisplayHint
			}
		}
	}
	return nil
}

// hintArtifact returns art with Display populated when art is a
// ToolCall with a registered DisplayHint. It returns art unchanged
// when art is not a ToolCall, no matching tool is registered, the
// tool has no DisplayHint, the Arguments are not valid JSON, or the
// hint returns nil. The function is idempotent: calling it on an
// already-hinted ToolCall re-derives the same Display value (since
// DisplayHint is documented as a pure function over its args).
func hintArtifact(ctx context.Context, art artifact.Artifact, opts []provider.InvokeOption) artifact.Artifact {
	tc, ok := art.(artifact.ToolCall)
	if !ok {
		return art
	}
	hint := hintForName(ctx, tc.Name, opts)
	if hint == nil {
		return art
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		return art
	}
	if v := hint(args); v != nil {
		tc.Display = v
		return tc
	}
	return art
}
