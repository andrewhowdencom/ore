// Command mock-server is a wire-compatible LLM mock that serves canned
// responses for either the OpenAI or Anthropic streaming wire format.
// Application code points a real provider at the bound URL (printed
// to stderr) to exercise the framework end-to-end without paying real
// LLM latency.
//
// Usage:
//
//	mock-server -vendor=openai -config=responses.json
//	mock-server -vendor=anthropic -config=responses.json -addr=:8080
//
// The JSON config is an array of mock.Response values; the server
// rotates through the array per HTTP request, wrapping on overflow.
//
// Both vendors are supported under one binary because the underlying
// SSE translation is structurally identical (queue of canned
// responses, hand-rolled SSE frame writer). The dispatch is selected
// by the -vendor flag at startup.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/andrewhowdencom/ore/x/provider/mock"
	anthropicmock "github.com/andrewhowdencom/ore/x/provider/mock/anthropic"
	openaimock "github.com/andrewhowdencom/ore/x/provider/mock/openai"
)

const usage = `mock-server: wire-compatible LLM mock

Usage:
  mock-server -vendor=<openai|anthropic> -config=<path-to-json> [-addr=<listen-addr>]

Flags:
  -vendor   (required) wire format to speak: "openai" or "anthropic"
  -config   (required) path to a JSON array of mock.Response values
  -addr     (optional) listen address; default ":0" (ephemeral port)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mock-server:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("mock-server", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	var (
		vendor = fs.String("vendor", "", "wire format to speak: openai or anthropic")
		config = fs.String("config", "", "path to a JSON array of mock.Response values")
		addr   = fs.String("addr", ":0", "listen address (default :0)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *vendor == "" || *config == "" {
		fs.Usage()
		return errors.New("-vendor and -config are required")
	}

	responses, err := loadResponses(*config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	handler, err := buildHandler(*vendor, responses)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen %q: %w", *addr, err)
	}

	// Print the bound URL in a parseable single-line format that the
	// workshop and tests can grep for.
	fmt.Fprintf(os.Stderr, "mock-server: listening on http://%s (vendor=%s)\n", ln.Addr().String(), *vendor)

	srv := &http.Server{Handler: handler}

	// Signal handling: SIGTERM/SIGINT triggers a graceful shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// Best-effort graceful shutdown.
		_ = srv.Shutdown(context.Background())
		return nil
	}
}

// loadResponses reads the JSON config and returns the parsed list.
// Validation: every entry must round-trip through the
// [mock.Response] shape; a malformed entry is reported with its
// position so the operator can fix the source.
func loadResponses(path string) ([]mock.Response, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rs []mock.Response
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return rs, nil
}

// buildHandler dispatches by vendor. Each vendor's Server.Handler()
// returns an http.Handler that emits the wire format the real SDK
// expects.
func buildHandler(vendor string, rs []mock.Response) (http.Handler, error) {
	switch vendor {
	case "openai":
		s, err := openaimock.New(openaimock.WithResponses(rs...))
		if err != nil {
			return nil, fmt.Errorf("openai: %w", err)
		}
		return s.Handler(), nil
	case "anthropic":
		s, err := anthropicmock.New(anthropicmock.WithResponses(rs...))
		if err != nil {
			return nil, fmt.Errorf("anthropic: %w", err)
		}
		return s.Handler(), nil
	default:
		return nil, fmt.Errorf("unknown vendor %q (want openai or anthropic)", vendor)
	}
}