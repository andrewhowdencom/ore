package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// loadConfig initializes Viper, reads the config file, and sets up defaults,
// flag bindings, and env var bindings.
func loadConfig(cmd *cobra.Command, v *viper.Viper, conduits []ConduitRegistration, handlers []HandlerRegistration) error {
	v.SetEnvPrefix("ORE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))

	// Manually bind global env vars
	v.BindEnv("config", "ORE_CONFIG")
	v.BindEnv("log_level", "ORE_LOG_LEVEL")
	v.BindEnv("api_key", "ORE_API_KEY")
	v.BindEnv("model", "ORE_MODEL")
	v.BindEnv("base_url", "ORE_BASE_URL")
	v.BindEnv("store_dir", "ORE_STORE_DIR")

	// Set global defaults
	v.SetDefault("log_level", "info")
	v.SetDefault("model", "gpt-4o")

	// Set conduit and handler defaults, bind flags and env vars
	for _, reg := range conduits {
		for k, val := range reg.Defaults {
			key := fmt.Sprintf("conduits.%s.%s", reg.Name, k)
			flagName := fmt.Sprintf("%s-%s", reg.Name, k)

			v.SetDefault(key, val)

			if f := cmd.Flags().Lookup(flagName); f != nil {
				if err := v.BindPFlag(key, f); err != nil {
					return fmt.Errorf("bind flag %s: %w", flagName, err)
				}
			}

			envVar := fmt.Sprintf("ORE_CONDUIT_%s_%s",
				strings.ToUpper(strings.ReplaceAll(reg.Name, "-", "_")),
				strings.ToUpper(strings.ReplaceAll(k, "-", "_")))
			if err := v.BindEnv(key, envVar); err != nil {
				return fmt.Errorf("bind env %s: %w", envVar, err)
			}
		}
	}

	for _, reg := range handlers {
		for k, val := range reg.Defaults {
			key := fmt.Sprintf("handlers.%s.%s", reg.Name, k)
			flagName := fmt.Sprintf("%s-%s", reg.Name, k)

			v.SetDefault(key, val)

			if f := cmd.Flags().Lookup(flagName); f != nil {
				if err := v.BindPFlag(key, f); err != nil {
					return fmt.Errorf("bind flag %s: %w", flagName, err)
				}
			}

			envVar := fmt.Sprintf("ORE_HANDLER_%s_%s",
				strings.ToUpper(strings.ReplaceAll(reg.Name, "-", "_")),
				strings.ToUpper(strings.ReplaceAll(k, "-", "_")))
			if err := v.BindEnv(key, envVar); err != nil {
				return fmt.Errorf("bind env %s: %w", envVar, err)
			}
		}
	}

	// Read config file
	configFile := v.GetString("config")
	if configFile != "" {
		if _, err := os.Stat(configFile); err == nil {
			v.SetConfigFile(configFile)
			if err := v.ReadInConfig(); err != nil {
				return fmt.Errorf("read config file %s: %w", configFile, err)
			}
		} else if cmd.Flags().Changed("config") {
			return fmt.Errorf("config file not found: %s", configFile)
		}
	}

	return nil
}
