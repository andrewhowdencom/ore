// Command benchmark runs an agent.Agent over a sequence of test
// cases and records per-case results as JSON Lines on stdout.
//
// The skeleton is deliberately minimal: a single agent, no
// concurrency, no metric aggregation, no statistical analysis.
// It exists to validate that the bundle abstraction supports "run
// an agent many times over inputs and record outputs." Future
// tasks can add concurrency, warmup, percentiles, and cross-case
// assertions.
//
// Configuration (env):
//
//	ORE_API_KEY              — required
//	ORE_MODEL                 — model name; e.g. "gpt-4o"
//	ORE_BASE_URL              — optional; defaults to the provider's default
//	BENCHMARK_CASES           — path to a JSON file with []Case
//	BENCHMARK_PATTERN         — "single_shot" (default) or "react"
//
// Case shape:
//
//	{ "id": "...", "input": "...", "spec": { "max_output_tokens": int } }
//
// Output shape (one JSON object per line on stdout):
//
//	{ "id": "...", "input": "...", "output": "...",
//	  "duration_ms": int, "error": "..."? }
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/provider/openai"
)

// Case is one input record loaded from BENCHMARK_CASES.
type Case struct {
	ID    string     `json:"id"`
	Input string     `json:"input"`
	Spec  *CaseSpec  `json:"spec,omitempty"`
}

// CaseSpec carries per-case overrides applied on top of the global
// model spec.
type CaseSpec struct {
	MaxOutputTokens int64 `json:"max_output_tokens"`
}

// Result is one JSON Lines record emitted to stdout.
type Result struct {
	ID         string `json:"id"`
	Input      string `json:"input"`
	Output     string `json:"output"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// ProviderBuilder builds a provider from credentials. Tests may
// inject a stub; production code uses the default openai builder.
type ProviderBuilder func(apiKey, baseURL string) (provider.Provider, error)

// OpenAIBuilder is the production provider builder. It is a
// package-level variable so tests can override it.
var OpenAIBuilder ProviderBuilder = func(apiKey, baseURL string) (provider.Provider, error) {
	opts := []openai.Option{openai.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	return openai.New(opts...)
}

func main() {
	if err := runMain(os.Stdout, os.Stderr, os.Getenv, OpenAIBuilder); err != nil {
		fmt.Fprintln(os.Stderr, "benchmark:", err)
		os.Exit(1)
	}
}

// runMain is the testable entry point. It reads env, builds the
// provider, loads cases, and emits per-case results to out.
func runMain(out, errOut io.Writer, getenv func(string) string, buildProv ProviderBuilder) error {
	apiKey := getenv("ORE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ORE_API_KEY not set")
	}
	model := getenv("ORE_MODEL")
	if model == "" {
		return fmt.Errorf("ORE_MODEL not set")
	}
	casesPath := getenv("BENCHMARK_CASES")
	if casesPath == "" {
		return fmt.Errorf("BENCHMARK_CASES not set")
	}

	cases, err := loadCases(casesPath)
	if err != nil {
		return fmt.Errorf("load cases: %w", err)
	}

	prov, err := buildProv(apiKey, getenv("ORE_BASE_URL"))
	if err != nil {
		return fmt.Errorf("build provider: %w", err)
	}

	patternName := getenv("BENCHMARK_PATTERN")
	if patternName == "" {
		patternName = "single_shot"
	}

	return run(out, prov, model, patternName, cases)
}

// loadCases reads and decodes a JSON file of cases.
func loadCases(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}

// run executes each case through a SingleShot agent (or a ReAct
// agent, if patternName == "react") and writes per-case JSON-Lines
// records to out. run is exported via package-internal access
// for tests.
func run(out io.Writer, prov provider.Provider, model, patternName string, cases []Case) error {
	spec := models.Spec{Name: model}

	enc := json.NewEncoder(out)
	for i := range cases {
		c := cases[i]
		caseSpec := spec
		if c.Spec != nil && c.Spec.MaxOutputTokens > 0 {
			caseSpec.MaxOutputTokens = c.Spec.MaxOutputTokens
		}

		a := agent.New("benchmark",
			agent.WithProvider(prov),
			agent.WithSpec(caseSpec),
			agent.WithPattern(buildPattern(patternName)),
		)

		res := runCase(context.Background(), a, c)

		if err := enc.Encode(res); err != nil {
			return fmt.Errorf("encode result: %w", err)
		}

		_ = a.Close()
	}
	return nil
}

// buildPattern returns a Pattern by name. Unknown names default to
// SingleShot (matching the documented default).
func buildPattern(name string) cognitive.Pattern {
	switch name {
	case "react":
		return &cognitive.ReAct{}
	default:
		return &cognitive.SingleShot{}
	}
}

// runCase executes a single case through the agent, capturing the
// produced turn via the agent's turn_complete event stream.
func runCase(ctx context.Context, a *agent.Agent, c Case) Result {
	start := time.Now()

	buf := &ledger.Buffer{}
	buf.Append(ledger.RoleUser, artifact.Text{Content: c.Input})

	type captured struct{ turn ledger.Turn }
	capturedCh := make(chan captured, 1)
	events := a.Subscribe("turn_complete")
	go func() {
		for ev := range events {
			if tc, ok := ev.(loop.TurnCompleteEvent); ok {
				select {
				case capturedCh <- captured{turn: tc.Turn}:
				default:
				}
				return
			}
		}
	}()

	res := Result{ID: c.ID, Input: c.Input}
	_, err := a.Run(ctx, buf)
	res.DurationMS = time.Since(start).Milliseconds()

	if err != nil {
		res.Error = err.Error()
		return res
	}

	select {
	case captured := <-capturedCh:
		res.Output = assistantText(captured.turn)
	case <-ctx.Done():
		res.Error = ctx.Err().Error()
	}
	return res
}

// assistantText concatenates Text and TextDelta content of a turn
// into a single string. Other artifact kinds are ignored.
func assistantText(turn ledger.Turn) string {
	var s string
	for _, a := range turn.Artifacts {
		switch v := a.(type) {
		case artifact.Text:
			s += v.Content
		case artifact.TextDelta:
			s += v.Content
		}
	}
	return s
}