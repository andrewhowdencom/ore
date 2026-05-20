package http

import (
	"github.com/mitchellh/mapstructure"
)

// Config holds forge-configurable options for the HTTP conduit.
type Config struct {
	Addr string `yaml:"addr"`
	UI   *bool  `yaml:"ui"`
}

// FromConfig converts a typed Config into functional options.
func FromConfig(cfg Config) []Option {
	var opts []Option
	if cfg.Addr != "" {
		opts = append(opts, WithAddr(cfg.Addr))
	}
	if cfg.UI != nil && !*cfg.UI {
		opts = append(opts, WithoutUI())
	}
	return opts
}

// OptionsFromMap bridges a YAML-decoded map to functional options via mapstructure.
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
