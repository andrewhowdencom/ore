package guardrails

import (
	"github.com/mitchellh/mapstructure"
)

// Config holds forge-configurable options for the guardrails transform.
type Config struct {
	Rules []string `yaml:"rules"`
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
		c.rules = rules
	}
}

// FromConfig converts a typed Config into functional options.
func FromConfig(cfg Config) []Option {
	var opts []Option
	if len(cfg.Rules) > 0 {
		opts = append(opts, WithRules(cfg.Rules...))
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
