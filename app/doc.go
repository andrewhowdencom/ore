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
//
// Environment variables follow the ORE_ prefix convention:
//
//   - ORE_API_KEY, ORE_MODEL, ORE_BASE_URL, ORE_STORE_DIR, ORE_LOG_LEVEL
//     for global settings
//   - ORE_CONDUIT_<NAME>_<KEY> for conduit-specific options
//     (e.g. ORE_CONDUIT_HTTP_ADDR for the "http" conduit's addr option)
//   - ORE_HANDLER_<NAME>_<KEY> for handler-specific options
//     (e.g. ORE_HANDLER_TOOLS_VERBOSE for the "tools" handler's verbose option)
//
// Dots and hyphens in names are normalised to underscores. For example,
// a conduit named "public-api" with an option "addr" becomes
// ORE_CONDUIT_PUBLIC_API_ADDR.
package app
