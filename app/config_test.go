package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "./config.yaml", "")
	cmd.Flags().String("log-level", "info", "")
	cmd.Flags().String("api-key", "", "")
	cmd.Flags().String("model", "gpt-4o", "")
	cmd.Flags().String("base-url", "", "")
	cmd.Flags().String("store-dir", "", "")

	v := viper.New()

	conduits := []ConduitRegistration{
		{
			Name:     "http",
			Defaults: map[string]any{"addr": ":8080", "ui": true},
		},
	}

	err := loadConfig(cmd, v, "./config.yaml", conduits, nil)
	require.NoError(t, err)

	assert.Equal(t, "info", v.GetString("log_level"))
	assert.Equal(t, "gpt-4o", v.GetString("model"))
	assert.Equal(t, ":8080", v.GetString("conduits.http.addr"))
	assert.Equal(t, true, v.GetBool("conduits.http.ui"))
}

func TestLoadConfig_ConfigFileOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(`
log_level: debug
conduits:
  http:
    addr: ":9090"
`), 0644)
	require.NoError(t, err)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", configPath, "")
	cmd.Flags().String("log-level", "info", "")
	cmd.Flags().String("api-key", "", "")
	cmd.Flags().String("model", "gpt-4o", "")
	cmd.Flags().String("base-url", "", "")
	cmd.Flags().String("store-dir", "", "")

	v := viper.New()

	conduits := []ConduitRegistration{
		{
			Name:     "http",
			Defaults: map[string]any{"addr": ":8080"},
		},
	}

	err = loadConfig(cmd, v, configPath, conduits, nil)
	require.NoError(t, err)

	assert.Equal(t, "debug", v.GetString("log_level"))
	assert.Equal(t, ":9090", v.GetString("conduits.http.addr"))
}

func TestLoadConfig_MissingDefaultConfig(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "./nonexistent-config.yaml", "")
	cmd.Flags().String("log-level", "info", "")
	cmd.Flags().String("api-key", "", "")
	cmd.Flags().String("model", "gpt-4o", "")
	cmd.Flags().String("base-url", "", "")
	cmd.Flags().String("store-dir", "", "")

	v := viper.New()

	err := loadConfig(cmd, v, "./nonexistent-config.yaml", nil, nil)
	require.NoError(t, err)
}

func TestLoadConfig_MissingExplicitConfig(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("log-level", "info", "")
	cmd.Flags().String("api-key", "", "")
	cmd.Flags().String("model", "gpt-4o", "")
	cmd.Flags().String("base-url", "", "")
	cmd.Flags().String("store-dir", "", "")

	require.NoError(t, cmd.Flags().Set("config", "/nonexistent/path/config.yaml"))

	v := viper.New()

	err := loadConfig(cmd, v, "/nonexistent/path/config.yaml", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config file not found")
}

func TestLoadConfig_EnvVarOverride(t *testing.T) {
	t.Setenv("ORE_API_KEY", "env-key")
	t.Setenv("ORE_MODEL", "env-model")

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("log-level", "info", "")
	cmd.Flags().String("api-key", "", "")
	cmd.Flags().String("model", "gpt-4o", "")
	cmd.Flags().String("base-url", "", "")
	cmd.Flags().String("store-dir", "", "")

	v := viper.New()

	err := loadConfig(cmd, v, "", nil, nil)
	require.NoError(t, err)

	assert.Equal(t, "env-key", v.GetString("api_key"))
	assert.Equal(t, "env-model", v.GetString("model"))
}

func TestLoadConfig_ConduitEnvVarOverride(t *testing.T) {
	t.Setenv("ORE_CONDUIT_HTTP_ADDR", ":9091")

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("log-level", "info", "")
	cmd.Flags().String("api-key", "", "")
	cmd.Flags().String("model", "gpt-4o", "")
	cmd.Flags().String("base-url", "", "")
	cmd.Flags().String("store-dir", "", "")

	v := viper.New()

	conduits := []ConduitRegistration{
		{
			Name:     "http",
			Defaults: map[string]any{"addr": ":8080"},
		},
	}

	err := loadConfig(cmd, v, "", conduits, nil)
	require.NoError(t, err)

	assert.Equal(t, ":9091", v.GetString("conduits.http.addr"))
}

func TestLoadConfig_FlagOverride(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("log-level", "info", "")
	cmd.Flags().String("api-key", "", "")
	cmd.Flags().String("model", "gpt-4o", "")
	cmd.Flags().String("base-url", "", "")
	cmd.Flags().String("store-dir", "", "")

	// Register the conduit flag as runWithArgs would
	cmd.Flags().String("http-addr", ":8080", "")
	require.NoError(t, cmd.Flags().Set("http-addr", ":9090"))
	// cmd.Flags().Set marks the flag as Changed=true automatically

	v := viper.New()

	conduits := []ConduitRegistration{
		{
			Name:     "http",
			Defaults: map[string]any{"addr": ":8080"},
		},
	}

	err := loadConfig(cmd, v, "", conduits, nil)
	require.NoError(t, err)

	assert.Equal(t, ":9090", v.GetString("conduits.http.addr"))
}

func TestGetOpts(t *testing.T) {
	v := viper.New()
	v.Set("conduits.http", map[string]any{"addr": ":8080", "ui": true})

	opts := getOpts(v, "conduits", "http")
	require.NotNil(t, opts)
	assert.Equal(t, ":8080", opts["addr"])
	assert.Equal(t, true, opts["ui"])

	opts = getOpts(v, "conduits", "tui")
	assert.Nil(t, opts)
}
