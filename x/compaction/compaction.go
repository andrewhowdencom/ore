package compaction

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
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
	originalTurns := turns
	var err error
	for _, s := range c.strategies {
		turns, err = s.Compact(ctx, turns)
		if err != nil {
			return originalTurns, false, fmt.Errorf("compaction strategy failed: %w", err)
		}
	}
	return turns, true, nil
}

// ForceCompact executes the configured strategies directly without evaluating
// the trigger. If no strategies are configured, it returns the original turns
// and false. It returns the compacted turns, true if strategies were applied,
// and any error from the strategies.
func (c *Compactor) ForceCompact(ctx context.Context, turns []state.Turn) ([]state.Turn, bool, error) {
	if len(c.strategies) == 0 {
		return turns, false, nil
	}
	originalTurns := turns
	var err error
	for _, s := range c.strategies {
		turns, err = s.Compact(ctx, turns)
		if err != nil {
			return originalTurns, false, fmt.Errorf("compaction strategy failed: %w", err)
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

// TokenUsageTrigger fires when the most recent artifact.Usage in the turn
// slice indicates total tokens exceed the compactor's Spec.Window.
type TokenUsageTrigger struct {
	Spec models.Spec
}

// ShouldCompact scans the turn slice from the end for the most recent
// artifact.Usage. If found and Usage.TotalTokens > Spec.Window, it returns
// true. If no Usage artifact is present, or Spec.Window is zero, it
// returns false (graceful degradation).
func (t TokenUsageTrigger) ShouldCompact(turns []state.Turn) bool {
	if t.Spec.Window == 0 {
		return false
	}
	for i := len(turns) - 1; i >= 0; i-- {
		for _, art := range turns[i].Artifacts {
			if u, ok := art.(artifact.Usage); ok {
				return u.TotalTokens > t.Spec.Window
			}
		}
	}
	return false
}
