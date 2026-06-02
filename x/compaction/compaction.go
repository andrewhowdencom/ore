package compaction

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
)

// Trigger decides whether compaction should run for a given turn slice.
type Trigger interface {
	ShouldCompact(turns []state.Turn) bool
}

// Strategy reduces a turn slice to a smaller turn slice.
type Strategy interface {
	Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error)
}

// Compactor coordinates a Trigger and a Strategy to optionally reduce state.
type Compactor struct {
	trigger  Trigger
	strategy Strategy
}

// Option configures a Compactor.
type Option func(*Compactor)

// WithTrigger sets the trigger that decides when to compact.
func WithTrigger(t Trigger) Option {
	return func(c *Compactor) {
		c.trigger = t
	}
}

// WithStrategy sets the strategy that reduces the turn slice.
func WithStrategy(s Strategy) Option {
	return func(c *Compactor) {
		c.strategy = s
	}
}

// New creates a Compactor with the provided options.
// If no trigger or strategy is provided, MaybeCompact never compacts.
func New(opts ...Option) *Compactor {
	c := &Compactor{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// MaybeCompact evaluates the configured Trigger. If it fires, it runs the
// Strategy and returns the compacted turns along with true. If the Trigger
// does not fire, it returns the original turns and false.
func (c *Compactor) MaybeCompact(ctx context.Context, turns []state.Turn) ([]state.Turn, bool, error) {
	if c.trigger == nil || c.strategy == nil {
		return turns, false, nil
	}
	if !c.trigger.ShouldCompact(turns) {
		return turns, false, nil
	}
	compact, err := c.strategy.Compact(ctx, turns)
	if err != nil {
		return nil, false, fmt.Errorf("compaction strategy failed: %w", err)
	}
	return compact, true, nil
}

// TurnCountTrigger fires when the number of turns exceeds a threshold.
type TurnCountTrigger struct {
	N int
}

// ShouldCompact returns true if len(turns) > t.N.
func (t TurnCountTrigger) ShouldCompact(turns []state.Turn) bool {
	return len(turns) > t.N
}

// KeepLastN drops all but the last N turns.
type KeepLastN struct {
	N int
}

// Compact returns the last N turns. If len(turns) <= N, it returns a copy of
// the full slice. The returned slice is always a distinct backing array from
// the input.
func (k KeepLastN) Compact(_ context.Context, turns []state.Turn) ([]state.Turn, error) {
	if k.N <= 0 {
		return nil, fmt.Errorf("KeepLastN.N must be > 0, got %d", k.N)
	}
	if len(turns) <= k.N {
		result := make([]state.Turn, len(turns))
		copy(result, turns)
		return result, nil
	}
	start := len(turns) - k.N
	result := make([]state.Turn, k.N)
	copy(result, turns[start:])
	return result, nil
}

// TokenUsageTrigger fires when the most recent artifact.Usage in the turn
// slice indicates total tokens exceed MaxTokens.
type TokenUsageTrigger struct {
	MaxTokens int
}

// ShouldCompact scans the turn slice from the end for the most recent
// artifact.Usage. If found and Usage.TotalTokens > MaxTokens, it returns true.
// If no Usage artifact is present, it returns false (graceful degradation).
func (t TokenUsageTrigger) ShouldCompact(turns []state.Turn) bool {
	for i := len(turns) - 1; i >= 0; i-- {
		for _, art := range turns[i].Artifacts {
			if u, ok := art.(artifact.Usage); ok {
				return u.TotalTokens > t.MaxTokens
			}
		}
	}
	return false
}
