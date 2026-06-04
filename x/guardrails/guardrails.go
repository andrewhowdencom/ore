package guardrails

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Transform prepends safety and formatting guardrails to the inference
// context. It implements loop.Transform, injecting each rule as a
// RoleUser turn without mutating the underlying persistent buffer.
//
// Guardrails are injected as RoleUser turns so they carry the weight of
// user instructions, distinct from the RoleSystem persona set by
// x/systemprompt.
type Transform struct {
	rules []string
}

// config holds the internal options for the Transform.
type config struct {
	rules []string
}

// Option configures the Transform.
type Option func(*config)

// WithRules sets the guardrail rules.
func WithRules(rules ...string) Option {
	return func(c *config) {
		c.rules = append(c.rules, rules...)
	}
}

// New creates a guardrails transform with the given options.
func New(opts ...Option) (loop.Transform, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Transform{rules: cfg.rules}, nil
}

// Transform implements loop.Transform. It returns a state view with
// RoleUser turns prepended before the base state's turns, one per rule.
func (t *Transform) Transform(ctx context.Context, st state.State) (state.State, error) {
	if len(t.rules) == 0 {
		return st, nil
	}

	virtual := make([]state.Turn, 0, len(t.rules))
	for _, rule := range t.rules {
		virtual = append(virtual, state.Turn{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{artifact.Text{Content: rule}},
		})
	}
	return state.Prepend(st, virtual), nil
}

// Rules returns the configured guardrail rules.
func (t *Transform) Rules() []string {
	return append([]string(nil), t.rules...)
}
