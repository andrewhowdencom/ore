package systemprompt

import (
	"github.com/mitchellh/mapstructure"
)

// Config holds forge-configurable options for the system prompt transform.
type Config struct {
	Content string `yaml:"content"`
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

// FromConfig converts a typed Config into functional options.
func FromConfig(cfg Config) []Option {
	var opts []Option
	if cfg.Content != "" {
		opts = append(opts, WithContent(cfg.Content))
	}
	return opts
}

// OptionsFromMap bridges a YAML-decoded map to functional options via
// mapstructure. This is the entry point used by Forge-generated code.
func OptionsFromMap(m map[string]any) ([]Option, error) {
	var cfg Config
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName: "yaml",
		Result:  &cfg,
	})
	if err != nil {
		return nil, err
	}
	if err := decoder.Decode(m); err != nil {
		return nil, err
	}
	return FromConfig(cfg), nil
}
