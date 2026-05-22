package systemprompt

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Transform prepends a static system prompt to the inference context.
// It implements loop.Transform, injecting a RoleSystem turn with a
// text artifact without mutating the underlying persistent buffer.
type Transform struct {
	content string
}

// config holds the internal options for the Transform.
type config struct {
	content string
}

// Option configures the Transform.
type Option func(*config)

// WithContent sets the system prompt content.
func WithContent(content string) Option {
	return func(c *config) {
		c.content = content
	}
}

// New creates a system prompt transform with the given options.
func New(opts ...Option) (loop.Transform, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Transform{content: cfg.content}, nil
}

// Transform implements loop.Transform. It returns a state view with a
// RoleSystem turn prepended before the base state's turns.
func (t *Transform) Transform(ctx context.Context, st state.State) (state.State, error) {
	virtual := []state.Turn{
		{
			Role:      state.RoleSystem,
			Artifacts: []artifact.Artifact{artifact.Text{Content: t.content}},
		},
	}
	return state.NewVirtualTurnState(st, virtual), nil
}
