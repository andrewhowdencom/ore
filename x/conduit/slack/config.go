package slack

import (
	"github.com/mitchellh/mapstructure"
)

// Config holds forge-configurable options for the Slack conduit.
type Config struct {
	BotToken  string `yaml:"bot_token"`
	AppToken  string `yaml:"app_token"`
	EventsAPI *bool  `yaml:"events_api"`
}

// FromConfig converts a typed Config into functional options.
func FromConfig(cfg Config) []Option {
	var opts []Option
	if cfg.BotToken != "" {
		opts = append(opts, WithBotToken(cfg.BotToken))
	}
	if cfg.AppToken != "" {
		opts = append(opts, WithAppToken(cfg.AppToken))
	}
	if cfg.EventsAPI != nil && *cfg.EventsAPI {
		opts = append(opts, WithEventsAPI())
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
