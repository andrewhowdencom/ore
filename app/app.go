package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Option configures app.Run.
type Option func(*appConfig)

// WithConduit registers a conduit factory and its compile-time defaults.
func WithConduit(name string, factory ConduitFactory, defaults map[string]any) Option {
	return func(cfg *appConfig) {
		cfg.conduits = append(cfg.conduits, ConduitRegistration{
			Name:     name,
			Factory:  factory,
			Defaults: defaults,
		})
	}
}

// WithHandler registers a handler factory and its compile-time defaults.
func WithHandler(name string, factory HandlerFactory, defaults map[string]any) Option {
	return func(cfg *appConfig) {
		cfg.handlers = append(cfg.handlers, HandlerRegistration{
			Name:     name,
			Factory:  factory,
			Defaults: defaults,
		})
	}
}

type appConfig struct {
	name                 string
	conduits             []ConduitRegistration
	handlers             []HandlerRegistration
	providerFactory      func(apiKey, model, baseURL string) (provider.Provider, error)
	storeFactory         func(dir string) (thread.Store, error)
	contextFactory       func() (context.Context, func())
	turnProcessorFactory func() session.TurnProcessor
}

// Run is the main entry point for generated agent binaries.
func Run(opts ...Option) error {
	cfg := &appConfig{
		name:                 "agent",
		providerFactory:      defaultProviderFactory,
		storeFactory:         defaultStoreFactory,
		turnProcessorFactory: defaultTurnProcessor,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return runWithArgs(os.Args[1:], cfg)
}

func defaultTurnProcessor() session.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		return step.Turn(ctx, st, prov)
	}
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// parseLogLevel converts a string to a slog.Level.
func parseLogLevel(s string) (slog.Level, error) {
	switch s {
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

func (cfg *appConfig) conduitFns(mgr *session.Manager, opts map[string]map[string]any) ([]conduit.Conduit, error) {
	var out []conduit.Conduit
	for _, reg := range cfg.conduits {
		optsMap := make(map[string]any)
		for k, v := range reg.Defaults {
			optsMap[k] = v
		}
		if o, ok := opts[reg.Name]; ok {
			for k, v := range o {
				optsMap[k] = v
			}
		}
		c, err := reg.Factory(mgr, optsMap)
		if err != nil {
			return nil, fmt.Errorf("create conduit %s: %w", reg.Name, err)
		}
		out = append(out, c)
	}
	return out, nil
}

// runWithArgs wires flags, parses args, loads config, and starts the
// agent.  It never returns nil on success because the command runs
// until interrupted.
func runWithArgs(args []string, cfg *appConfig) error {
	var configFile string
	var logLevel string

	v := viper.New()

	cmd := &cobra.Command{
		Use:           cfg.name,
		Short:         fmt.Sprintf("Run the %s agent", cfg.name),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := loadConfig(cmd, v, configFile, cfg.conduits, cfg.handlers); err != nil {
				return err
			}
			return runAgent(cmd, v, cfg)
		},
	}

	cmd.SetArgs(args)

	// Global flags.
	cmd.Flags().StringVar(&configFile, "config", "./config.yaml", "Path to config file")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug|info|warn|error)")
	cmd.Flags().String("api-key", "", "OpenAI API key ($ORE_API_KEY)")
	cmd.Flags().String("model", "gpt-4o", "LLM model name")
	cmd.Flags().String("base-url", "", "Provider base URL (for local LLMs)")
	cmd.Flags().String("store-dir", "", "Thread store directory")

	// Conduit flags (defaults from forge.yaml).
	for _, reg := range cfg.conduits {
		for k, val := range reg.Defaults {
			flagName := fmt.Sprintf("%s-%s", reg.Name, k)
			cmd.Flags().String(flagName, fmt.Sprint(val), fmt.Sprintf("%s conduit %s", reg.Name, k))
		}
	}

	// Handler flags (defaults from forge.yaml).
	for _, reg := range cfg.handlers {
		for k, val := range reg.Defaults {
			flagName := fmt.Sprintf("%s-%s", reg.Name, k)
			cmd.Flags().String(flagName, fmt.Sprint(val), fmt.Sprintf("%s handler %s", reg.Name, k))
		}
	}

	return cmd.Execute()
}

// runAgent builds the provider, session manager, conduits, and starts the
// agent lifecycle.  It blocks until the context is cancelled.
func runAgent(cmd *cobra.Command, v *viper.Viper, cfg *appConfig) error {
	// --- Log level ---
	logLevel := v.GetString("log_level")
	level, err := parseLogLevel(logLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	// --- Provider ---
	apiKey := v.GetString("api_key")
	if apiKey == "" {
		return fmt.Errorf("api-key is required (flag, ORE_API_KEY env var, or config file)")
	}
	model := v.GetString("model")
	baseURL := v.GetString("base_url")
	storeDir := v.GetString("store_dir")

	if cfg.providerFactory == nil {
		cfg.providerFactory = defaultProviderFactory
	}
	p, err := cfg.providerFactory(apiKey, model, baseURL)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	// --- Store ---
	if cfg.storeFactory == nil {
		cfg.storeFactory = defaultStoreFactory
	}
	store, err := cfg.storeFactory(storeDir)
	if err != nil {
		return fmt.Errorf("create store: %w", err)
	}

	// --- Handler option maps (pre-computed from Viper) ---
	handlerOptMaps := make(map[string]map[string]any)
	for _, reg := range cfg.handlers {
		optsMap := make(map[string]any)
		for k, v := range reg.Defaults {
			optsMap[k] = v
		}
		for k := range reg.Defaults {
			key := fmt.Sprintf("handlers.%s.%s", reg.Name, k)
			val := v.Get(key)
			if val != nil {
				optsMap[k] = val
			}
		}
		handlerOptMaps[reg.Name] = optsMap
	}

	// --- Step factory (lazy handler creation) ---
	if cfg.turnProcessorFactory == nil {
		cfg.turnProcessorFactory = defaultTurnProcessor
	}
	stepFactory := func() (*loop.Step, error) {
		var handlers []loop.Handler
		for _, reg := range cfg.handlers {
			optsMap := make(map[string]any)
			for k, v := range reg.Defaults {
				optsMap[k] = v
			}
			if o, ok := handlerOptMaps[reg.Name]; ok {
				for k, v := range o {
					optsMap[k] = v
				}
			}
			h, err := reg.Factory(optsMap)
			if err != nil {
				return nil, fmt.Errorf("create handler %s: %w", reg.Name, err)
			}
			handlers = append(handlers, h)
		}
		return loop.New(loop.WithHandlers(handlers...)), nil
	}

	// --- Session manager ---
	mgr := session.NewManager(store, p, stepFactory, cfg.turnProcessorFactory())

	// --- Conduit options ---
	conduitOpts := make(map[string]map[string]any)
	for _, reg := range cfg.conduits {
		optsMap := make(map[string]any)
		for k := range reg.Defaults {
			key := fmt.Sprintf("conduits.%s.%s", reg.Name, k)
			val := v.Get(key)
			if val == nil {
				continue
			}
			optsMap[k] = val
		}
		conduitOpts[reg.Name] = optsMap
	}

	// --- Create conduits ---
	conduits, err := cfg.conduitFns(mgr, conduitOpts)
	if err != nil {
		return err
	}

	// --- Agent ---
	a := agent.New(mgr)
	for _, c := range conduits {
		a.Add(c)
	}

	cmdCtx := cmd.Context()
	if cmdCtx == nil {
		cmdCtx = context.Background()
	}
	ctx, stop := signal.NotifyContext(cmdCtx, syscall.SIGINT, syscall.SIGTERM)
	if cfg.contextFactory != nil {
		ctx, stop = cfg.contextFactory()
	}
	defer stop()

	return a.Run(ctx)
}
