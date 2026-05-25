package systemprompt

import (
	"context"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Transform prepends a system prompt to the inference context.
// It implements loop.Transform, injecting a RoleSystem turn with a
// text artifact without mutating the underlying persistent buffer.
type Transform struct {
	contentFuncs []func() string
}

// config holds the internal options for the Transform.
type config struct {
	contentFuncs []func() string
}

// Option configures the Transform.
type Option func(*config)

// WithContentFunc adds a single function that returns a prompt fragment.
// Multiple calls accumulate; fragments are evaluated in order and
// concatenated with "\n\n" separators on each Transform call.
func WithContentFunc(fn func() string) Option {
	return func(c *config) {
		if fn != nil {
			c.contentFuncs = append(c.contentFuncs, fn)
		}
	}
}

// WithContentFuncs adds multiple functions that return prompt fragments.
// Fragments are evaluated in order and concatenated with "\n\n"
// separators on each Transform call. Nil functions are skipped.
func WithContentFuncs(fns ...func() string) Option {
	return func(c *config) {
		for _, fn := range fns {
			if fn != nil {
				c.contentFuncs = append(c.contentFuncs, fn)
			}
		}
	}
}

// New creates a system prompt transform with the given options.
// It currently always returns a non-nil Transform and a nil error;
// the error return is reserved for future validation.
func New(opts ...Option) (loop.Transform, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Transform{contentFuncs: cfg.contentFuncs}, nil
}

// Transform implements loop.Transform. It returns a state view with a
// RoleSystem turn prepended before the base state's turns.
// Fragments are evaluated in registration order, empty results are omitted,
// and non-empty results are joined with "\n\n".
func (t *Transform) Transform(ctx context.Context, st state.State) (state.State, error) {
	var parts []string
	for _, fn := range t.contentFuncs {
		if fn == nil {
			continue
		}
		if s := fn(); s != "" {
			parts = append(parts, s)
		}
	}

	virtual := []state.Turn{
		{
			Role:      state.RoleSystem,
			Artifacts: []artifact.Artifact{artifact.Text{Content: strings.Join(parts, "\n\n")}},
		},
	}
	return state.NewVirtualTurnState(st, virtual), nil
}
