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

// ChainStrategy implements Strategy by running multiple strategies in
// sequence, piping the output of each strategy into the input of the next.
type ChainStrategy struct {
	strategies []Strategy
}

// NewChainStrategy creates a ChainStrategy from the provided strategies.
func NewChainStrategy(strategies ...Strategy) ChainStrategy {
	return ChainStrategy{strategies: strategies}
}

// Compact runs each strategy in order, passing the output of strategy N as
// the input to strategy N+1. If any strategy fails, the chain stops and
// returns the error.
func (c ChainStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error) {
	var err error
	for i, s := range c.strategies {
		if s == nil {
			return nil, fmt.Errorf("strategy at index %d is nil", i)
		}
		turns, err = s.Compact(ctx, turns)
		if err != nil {
			return nil, err
		}
	}
	return turns, nil
}

// Compactor coordinates a Trigger and a Strategy to optionally reduce state.
type Compactor struct {
	trigger    Trigger
	strategies []Strategy
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
// Multiple calls accumulate; the order of calls determines execution order.
func WithStrategy(s Strategy) Option {
	return func(c *Compactor) {
		if s != nil {
			c.strategies = append(c.strategies, s)
		}
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
	if c.trigger == nil || len(c.strategies) == 0 {
		return turns, false, nil
	}
	if !c.trigger.ShouldCompact(turns) {
		return turns, false, nil
	}
	var err error
	for _, s := range c.strategies {
		turns, err = s.Compact(ctx, turns)
		if err != nil {
			return nil, false, fmt.Errorf("compaction strategy failed: %w", err)
		}
	}
	return turns, true, nil
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
