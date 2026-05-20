// Package app provides Cobra/Viper-backed scaffolding for forge-generated
// binaries. It centralizes CLI flag parsing, configuration file loading,
// environment variable binding, and agent lifecycle management so that
// generated main.go files are extremely thin.
//
// A generated binary only needs to register its conduits and handlers:
//
//	func main() {
//		app.Run(
//			app.WithConduit("http", func(mgr *session.Manager, opts map[string]any) (conduit.Conduit, error) {
//				cOpts, err := http.OptionsFromMap(opts)
//				if err != nil {
//					return nil, err
//				}
//				return http.New(mgr, cOpts...)
//			}, map[string]any{"addr": ":8080"}),
//		)
//	}
//
// The app package handles the rest: parsing --config, --api-key, --model,
// --base-url, --store-dir, and --log-level flags; reading ORE_* environment
// variables; loading a YAML/JSON/TOML config file; merging layers with the
// precedence CLI flags > env vars > config file > compile-time defaults;
// creating the OpenAI provider, thread store, session manager, and agent;
// instantiating conduits with merged runtime options; and running the agent
// until interrupt.
package app
