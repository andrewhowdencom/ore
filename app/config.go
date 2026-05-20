package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// loadConfig wires all configuration sources into the provided Viper
// instance.  It mutates Viper in place so that later reads via
// v.GetString / v.GetBool / … see the merged result.
//
// The precedence order is:
//
//   1. Explicitly-set CLI flags (synced with v.Set)
//   2. Environment variables
//   3. Config file
//   4. Compile-time defaults
//
// The configFile parameter is the value of the --config flag (which may
// be the default).  It is read directly because Viper's BindPFlag does
// not work correctly with local viper.New() instances when flags are
// parsed after binding.  Explicit flag syncing is used for all other flags
// as well.
func loadConfig(cmd *cobra.Command, v *viper.Viper, configFile string, conduits []ConduitRegistration, handlers []HandlerRegistration, transforms []TransformRegistration) error {
	v.SetEnvPrefix("ORE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))

	// Bind global environment variables.
	v.BindEnv("config", "ORE_CONFIG")
	v.BindEnv("log_level", "ORE_LOG_LEVEL")
	v.BindEnv("api_key", "ORE_API_KEY")
	v.BindEnv("model", "ORE_MODEL")
	v.BindEnv("base_url", "ORE_BASE_URL")
	v.BindEnv("store_dir", "ORE_STORE_DIR")

	// Set global defaults.
	v.SetDefault("log_level", "info")
	v.SetDefault("model", "gpt-4o")

	// Sync explicitly-set global flags to Viper so they take highest
	// precedence.
	if cmd.Flags().Changed("log-level") {
		v.Set("log_level", cmd.Flags().Lookup("log-level").Value.String())
	}
	if cmd.Flags().Changed("api-key") {
		v.Set("api_key", cmd.Flags().Lookup("api-key").Value.String())
	}
	if cmd.Flags().Changed("model") {
		v.Set("model", cmd.Flags().Lookup("model").Value.String())
	}
	if cmd.Flags().Changed("base-url") {
		v.Set("base_url", cmd.Flags().Lookup("base-url").Value.String())
	}
	if cmd.Flags().Changed("store-dir") {
		v.Set("store_dir", cmd.Flags().Lookup("store-dir").Value.String())
	}

	// Read config file if it exists.
	if configFile != "" {
		if _, err := os.Stat(configFile); err == nil {
			v.SetConfigFile(configFile)
			if err := v.ReadInConfig(); err != nil {
				return fmt.Errorf("read config file %s: %w", configFile, err)
			}
		} else if cmd.Flags().Changed("config") {
			// --config was explicitly provided but the file is missing.
			return fmt.Errorf("config file not found: %s", configFile)
		}
	}

	// Set conduit defaults, bind env vars, and sync changed flags.
	for _, reg := range conduits {
		for k, val := range reg.Defaults {
			key := fmt.Sprintf("conduits.%s.%s", reg.Name, k)
			flagName := fmt.Sprintf("%s-%s", reg.Name, k)

			v.SetDefault(key, val)

			if f := cmd.Flags().Lookup(flagName); f != nil && f.Changed {
				v.Set(key, f.Value.String())
			}

			envVar := fmt.Sprintf("ORE_CONDUIT_%s_%s",
				strings.ToUpper(strings.ReplaceAll(reg.Name, "-", "_")),
				strings.ToUpper(strings.ReplaceAll(k, "-", "_")))
			v.BindEnv(key, envVar)
		}
	}

	// Set handler defaults, bind env vars, and sync changed flags.
	for _, reg := range handlers {
		for k, val := range reg.Defaults {
			key := fmt.Sprintf("handlers.%s.%s", reg.Name, k)
			flagName := fmt.Sprintf("%s-%s", reg.Name, k)

			v.SetDefault(key, val)

			if f := cmd.Flags().Lookup(flagName); f != nil && f.Changed {
				v.Set(key, f.Value.String())
			}

			envVar := fmt.Sprintf("ORE_HANDLER_%s_%s",
				strings.ToUpper(strings.ReplaceAll(reg.Name, "-", "_")),
				strings.ToUpper(strings.ReplaceAll(k, "-", "_")))
			v.BindEnv(key, envVar)
		}
	}

	// Set transform defaults, bind env vars, and sync changed flags.
	for _, reg := range transforms {
		for k, val := range reg.Defaults {
			key := fmt.Sprintf("transforms.%s.%s", reg.Name, k)
			flagName := fmt.Sprintf("%s-%s", reg.Name, k)

			v.SetDefault(key, val)

			if f := cmd.Flags().Lookup(flagName); f != nil && f.Changed {
				v.Set(key, f.Value.String())
			}

			envVar := fmt.Sprintf("ORE_TRANSFORM_%s_%s",
				strings.ToUpper(strings.ReplaceAll(reg.Name, "-", "_")),
				strings.ToUpper(strings.ReplaceAll(k, "-", "_")))
			v.BindEnv(key, envVar)
		}
	}

	return nil
}
