package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type appConfig struct {
	conduits []ConduitRegistration
	handlers []HandlerRegistration

	// Test-only overrides
	providerFactory func(apiKey, model, baseURL string) (provider.Provider, error)
	storeFactory    func(dir string) (thread.Store, error)
	contextFactory  func() (context.Context, func())
}

// Option configures the app.
type Option func(*appConfig)

// WithConduit registers a conduit with the given name, factory, and
// compile-time defaults.
func WithConduit(name string, factory ConduitFactory, defaults map[string]any) Option {
	return func(c *appConfig) {
		c.conduits = append(c.conduits, ConduitRegistration{
			Name:     name,
			Factory:  factory,
			Defaults: defaults,
		})
	}
}

// WithHandler registers a handler with the given name, factory, and
// compile-time defaults.
func WithHandler(name string, factory HandlerFactory, defaults map[string]any) Option {
	return func(c *appConfig) {
		c.handlers = append(c.handlers, HandlerRegistration{
			Name:     name,
			Factory:  factory,
			Defaults: defaults,
		})
	}
}

// Run is the entry point for generated binaries. It parses CLI flags and
// environment variables, reads the config file, builds the agent, and runs
// until the process receives an interrupt signal.
func Run(opts ...Option) error {
	return runWithArgs(os.Args[1:], opts...)
}

func runWithArgs(args []string, opts ...Option) error {
	var cfg appConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.providerFactory == nil {
		cfg.providerFactory = defaultProviderFactory
	}
	if cfg.storeFactory == nil {
		cfg.storeFactory = func(dir string) (thread.Store, error) {
			if dir != "" {
				return thread.NewJSONStore(dir)
			}
			return thread.NewMemoryStore(), nil
		}
	}

	var (
		configFile string
		logLevel   string
		apiKey     string
		model      string
		baseURL    string
		storeDir   string
	)

	v := viper.New()

	cmd := &cobra.Command{
		Use:           "agent",
		Short:         "Generated ore agent",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfig(cmd, v, cfg.conduits, cfg.handlers); err != nil {
				return err
			}
			return runAgent(cmd, v, &cfg, configFile, logLevel, apiKey, model, baseURL, storeDir)
		},
	}

	// Global flags
	cmd.Flags().StringVar(&configFile, "config", "./config.yaml", "Path to configuration file")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "OpenAI API key")
	cmd.Flags().StringVar(&model, "model", "gpt-4o", "Model name")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "OpenAI-compatible base URL")
	cmd.Flags().StringVar(&storeDir, "store-dir", "", "Directory for JSON thread store")

	// Register conduit-specific flags
	for _, reg := range cfg.conduits {
		for k, val := range reg.Defaults {
			flagName := fmt.Sprintf("%s-%s", reg.Name, k)
			switch dval := val.(type) {
			case string:
				cmd.Flags().String(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			case bool:
				cmd.Flags().Bool(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			case int:
				cmd.Flags().Int(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			case int64:
				cmd.Flags().Int64(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			case float64:
				cmd.Flags().Float64(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			}
		}
	}

	// Register handler-specific flags
	for _, reg := range cfg.handlers {
		for k, val := range reg.Defaults {
			flagName := fmt.Sprintf("%s-%s", reg.Name, k)
			switch dval := val.(type) {
			case string:
				cmd.Flags().String(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			case bool:
				cmd.Flags().Bool(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			case int:
				cmd.Flags().Int(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			case int64:
				cmd.Flags().Int64(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			case float64:
				cmd.Flags().Float64(flagName, dval, fmt.Sprintf("%s %s", reg.Name, k))
			}
		}
	}

	cmd.SetArgs(args)
	return cmd.Execute()
}

func runAgent(cmd *cobra.Command, v *viper.Viper, cfg *appConfig, configFile, logLevel, apiKey, model, baseURL, storeDir string) error {
	// Set up logging
	level, err := parseLogLevel(v.GetString("log_level"))
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Validate required settings
	apiKey = v.GetString("api_key")
	if apiKey == "" {
		return fmt.Errorf("api-key is required (set --api-key, ORE_API_KEY, or api_key in config file)")
	}
	model = v.GetString("model")
	baseURL = v.GetString("base_url")
	storeDir = v.GetString("store_dir")

	// Create thread store
	store, err := cfg.storeFactory(storeDir)
	if err != nil {
		return fmt.Errorf("create thread store: %w", err)
	}

	// Create provider
	prov, err := cfg.providerFactory(apiKey, model, baseURL)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	// Create step factory
	stepFactory := func() (*loop.Step, error) {
		var handlerOpts []loop.Option
		for _, reg := range cfg.handlers {
			opts := getOpts(v, "handlers", reg.Name)
			h, err := reg.Factory(opts)
			if err != nil {
				return nil, fmt.Errorf("create handler %s: %w", reg.Name, err)
			}
			handlerOpts = append(handlerOpts, loop.WithHandlers(h))
		}
		return loop.New(handlerOpts...), nil
	}

	// Create session manager
	mgr := session.NewManager(store, prov, stepFactory, cognitive.NewTurnProcessor())

	// Create agent
	a := agent.New(mgr)

	// Instantiate conduits
	for _, reg := range cfg.conduits {
		opts := getOpts(v, "conduits", reg.Name)
		c, err := reg.Factory(mgr, opts)
		if err != nil {
			return fmt.Errorf("create conduit %s: %w", reg.Name, err)
		}
		a.Add(c)
	}

	// Run with signal handling (overridable in tests)
	var ctx context.Context
	var stop func()
	if cfg.contextFactory != nil {
		ctx, stop = cfg.contextFactory()
	} else {
		ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)
	}
	defer stop()

	return a.Run(ctx)
}

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid log level %q", s)
	}
}
