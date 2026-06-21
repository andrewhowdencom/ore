package systemprompt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"
)

// Transform prepends a system prompt to the inference context.
// It implements loop.Transform, injecting a RoleSystem turn with a
// text artifact without mutating the underlying persistent buffer.
type Transform struct {
	contentFuncs    []func() string
	ctxContentFuncs []func(context.Context) string
}

// config holds the internal options for the Transform.
type config struct {
	contentFuncs    []func() string
	ctxContentFuncs []func(context.Context) string
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

// Resolver is the contract for content sources whose value is determined
// lazily on each call. The system prompt transform invokes Resolve on
// every Transform, so a Resolver whose underlying state mutates between
// calls (e.g., via SetPath on a *source.FileResolver) appears in the
// prompt without re-registration.
//
// Register a Resolver with WithContentFunc(src.Resolve). Callers that
// need to "swap" the active content (for example, when the user switches
// role mid-session) mutate the resolver's state directly instead of
// stacking another WithContentFunc call — the original bug behind
// multi-identity agent contexts.
type Resolver interface {
	Resolve() string
}

// WithContextContentFunc adds a single function that receives the Transform
// context and returns a prompt fragment. Multiple calls accumulate;
// fragments are evaluated after all regular contentFuncs, in order,
// and concatenated with "\n\n" separators on each Transform call.
func WithContextContentFunc(fn func(context.Context) string) Option {
	return func(c *config) {
		if fn != nil {
			c.ctxContentFuncs = append(c.ctxContentFuncs, fn)
		}
	}
}

// WithToolExamples adds a content function that renders a markdown prompt
// fragment containing the few-shot examples for the given tools. Only tools
// with non-empty Examples are included. Each tool is rendered with its name,
// description, and then each example as an input JSON block, an output block,
// and an optional explanation. This is opt-in; applications may choose which
// tools to pass.
func WithToolExamples(tools []tool.Tool) Option {
	return WithContentFunc(func() string {
		var sections []string
		for _, t := range tools {
			if len(t.Examples) == 0 {
				continue
			}
			var lines []string
			lines = append(lines, fmt.Sprintf("## %s", t.Name))
			if t.Description != "" {
				lines = append(lines, t.Description)
			}
			for i, ex := range t.Examples {
				lines = append(lines, fmt.Sprintf("### Example %d", i+1))
				if in, err := json.MarshalIndent(ex.Input, "", "  "); err == nil {
					lines = append(lines, "Input:", "```json", string(in), "```")
				}
				if out, err := json.MarshalIndent(ex.Output, "", "  "); err == nil {
					lines = append(lines, "Output:", "```json", string(out), "```")
				} else if s, ok := ex.Output.(string); ok {
					lines = append(lines, "Output:", "```", s, "```")
				} else {
					lines = append(lines, "Output:", "```", fmt.Sprintf("%v", ex.Output), "```")
				}
				if ex.Explanation != "" {
					lines = append(lines, ex.Explanation)
				}
			}
			sections = append(sections, strings.Join(lines, "\n"))
		}
		if len(sections) == 0 {
			return ""
		}
		return "Tool Examples:\n\n" + strings.Join(sections, "\n\n")
	})
}

// New creates a system prompt transform with the given options.
// It currently always returns a non-nil Transform and a nil error;
// the error return is reserved for future validation.
func New(opts ...Option) (loop.Transform, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Transform{contentFuncs: cfg.contentFuncs, ctxContentFuncs: cfg.ctxContentFuncs}, nil
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
	for _, fn := range t.ctxContentFuncs {
		if fn == nil {
			continue
		}
		if s := fn(ctx); s != "" {
			parts = append(parts, s)
		}
	}

	virtual := []state.Turn{
		{
			Role:      state.RoleSystem,
			Artifacts: []artifact.Artifact{artifact.Text{Content: strings.Join(parts, "\n\n")}},
		},
	}
	return state.Prepend(st, virtual), nil
}
