package systemprompt

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Transform prepends a system prompt to the inference context.
// It implements loop.Transform, injecting a RoleSystem turn with a
// text artifact without mutating the underlying persistent buffer.
type Transform struct {
	contentFunc func() string
}

// config holds the internal options for the Transform.
type config struct {
	contentFunc func() string
}

// Option configures the Transform.
type Option func(*config)

// WithContentFunc sets a function that returns the system prompt content.
// The function is evaluated on every Transform call, enabling dynamic
// system prompts that can change between turns (e.g., from thread metadata).
func WithContentFunc(fn func() string) Option {
	return func(c *config) {
		c.contentFunc = fn
	}
}

// New creates a system prompt transform with the given options.
func New(opts ...Option) (loop.Transform, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.contentFunc == nil {
		cfg.contentFunc = func() string { return "" }
	}
	return &Transform{contentFunc: cfg.contentFunc}, nil
}

// Transform implements loop.Transform. It returns a state view with a
// RoleSystem turn prepended before the base state's turns.
func (t *Transform) Transform(ctx context.Context, st state.State) (state.State, error) {
	virtual := []state.Turn{
		{
			Role:      state.RoleSystem,
			Artifacts: []artifact.Artifact{artifact.Text{Content: t.contentFunc()}},
		},
	}
	return state.NewVirtualTurnState(st, virtual), nil
}
