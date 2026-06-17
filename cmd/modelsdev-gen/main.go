// Command modelsdev-gen regenerates the x/catalog/models/ subtree from the
// upstream models.dev catalog. Run via `go run ./cmd/modelsdev-gen` from
// the repo root, or via `task generate`.
//
// The generator is a pure function of its inputs:
//   - GET https://models.dev/api.json (the only network call)
//   - The hard-coded primaryProviders allowlist below
//
// It writes one file per primary provider under x/catalog/models/. The
// output is committed; the generator does not run on build. Re-run
// when models.dev ships new model revisions or when the allowlist grows.
//
// Stdlib only: net/http, encoding/json, go/format, text/template.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"
)

// modelsDevURL is the upstream catalog. The endpoint returns a single
// JSON object whose top-level keys are provider ids and whose values
// are provider entries (each with a "models" object keyed by model
// id). The schema is documented at https://models.dev.
const modelsDevURL = "https://models.dev/api.json"

// primaryProviders is the hand-maintained allowlist of vendors that
// the framework treats as first-class model families. Any provider
// id in this list gets its own catalog file. New vendors are added
// in an explicit commit so a stale models.dev snapshot cannot quietly
// grow the catalog.
var primaryProviders = []string{
	"anthropic",
	"openai",
	"google",
	"xai",
	"deepseek",
	"mistral",
	"minimax",
	"alibaba",
	"cohere",
	// "meta" exists as a family concept but models.dev exposes
	// the Meta models under the "llama" provider id. The two
	// are mapped to the same Meta family in the catalog
	// generator. The mapping is local to this generator.
	"llama",
}

// providerAliases maps models.dev provider ids to the family name
// the catalog file uses. A provider not in this map uses its
// models.dev id as-is. The only known alias today is "llama" ->
// "meta": Meta's models live under the "llama" key on models.dev
// but the family name (and the resulting file) is "meta".
var providerAliases = map[string]string{
	"llama": "meta",
}

// modelsDevProvider mirrors the upstream JSON shape. Only the fields
// the generator needs are decoded; everything else is ignored.
type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

// modelsDevModel mirrors the per-model fields the generator needs.
// A handful of upstream models omit "limit" entirely; those are
// skipped (we cannot emit a Spec without a Window).
type modelsDevModel struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Reasoning bool               `json:"reasoning"`
	Limit     *modelsDevLimit    `json:"limit,omitempty"`
}

// modelsDevLimit carries the context window and per-call output
// budget. The two are independent: a 200k-window model can still
// have a 64k output cap.
type modelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

// openRouterModelsURL is the upstream OpenRouter catalog. The
// endpoint returns {"data": [{"id": "<vendor>/<model>", ...}, ...]}.
// The id is the wire identifier OpenRouter routes; the framework's
// models.Spec.Name is mapped to it via the generated lookup.
const openRouterModelsURL = "https://openrouter.ai/api/v1/models"

// vercelModelsURL is the upstream Vercel AI Gateway catalog. The
// shape mirrors OpenRouter's: {"data": [{"id": "<vendor>/<model>", ...}, ...]}.
const vercelModelsURL = "https://ai-gateway.vercel.sh/v1/models"

// gatewayModel mirrors the per-model fields the gateway endpoints
// expose. Only ID is used for the canonical join; Name is captured
// for diagnostic logging only.
type gatewayModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// gatewayResponse is the top-level shape of both OpenRouter and
// Vercel's catalog endpoints. The "object" field Vercel adds (e.g.
// "list") is ignored.
type gatewayResponse struct {
	Data []gatewayModel `json:"data"`
}

// vendorPrefixesOpenRouter lists the vendor prefixes OpenRouter
// prepends to every model id. The framework strips each prefix in
// order and matches the remainder against the models.dev catalog.
// An entry without a recognized prefix is dropped from the join.
var vendorPrefixesOpenRouter = []string{
	"anthropic/",
	"openai/",
	"google/",
	"x-ai/",
	"deepseek/",
	"mistralai/",
	"meta-llama/",
	"qwen/",
	"cohere/",
	"minimax/",
}

// vendorPrefixesVercel lists the vendor prefixes the Vercel AI
// Gateway uses. Different gateways use different conventions
// (OpenRouter says "x-ai" and "mistralai"; Vercel says "xai" and
// "mistral"), so the lists are per-gateway.
var vendorPrefixesVercel = []string{
	"alibaba/",
	"anthropic/",
	"openai/",
	"google/",
	"xai/",
	"deepseek/",
	"mistral/",
	"meta/",
	"cohere/",
	"minimax/",
	"moonshotai/",
	"stepfun/",
}

// gatewayLookupTemplate produces one x/provider/<gateway>/lookup.go
// file. The map keys are sorted canonical names and the values are
// gateway wire identifiers; the join logic in joinCanonicalToGateway
// produces them in canonical-order, and go/format canonicalizes
// whitespace.
const gatewayLookupTemplate = `// Code generated by cmd/modelsdev-gen. DO NOT EDIT by hand.
//
// Source: {{.SourceURL}} (regenerated when the upstream {{.Vendor}}
// catalog changes). Run "task generate" from the repo root to refresh.
//
// The map is keyed by canonical [models.Spec.Name] (as emitted by
// x/catalog/models) and returns {{.Vendor}}'s wire identifier.
// On miss the resolver falls back to identity so callers can still
// request a model by its {{.Vendor}} wire id verbatim — useful for
// models not yet in the canonical catalog or {{.Vendor}}-only models.
package {{.Package}}

// nameLookup maps canonical [models.Spec.Name] values to {{.Vendor}}
// wire identifiers. Sorted by canonical key for stable diffs across
// regenerations.
var nameLookup = map[string]string{
{{- range .Entries }}
	{{ printf "%q: %q," .Canonical .Gateway }}
{{- end }}
}
`

// gatewayTemplateData is the per-gateway payload rendered into
// gatewayLookupTemplate.
type gatewayTemplateData struct {
	SourceURL string
	Vendor    string
	Package   string
	Entries   []gatewayEntry
}

// gatewayEntry is one (canonical, gateway) pair.
type gatewayEntry struct {
	Canonical string
	Gateway   string
}

// specTemplate is the source of truth for the per-family file
// format. It produces a single `var` per model, in alphabetical
// order by Go identifier. The `ptr` helper is provided by the
// hand-maintained helpers.go in the same package; the generator
// does not emit it (one helper for the whole catalog, not one
// per family).
//
// Temperature is hard-coded to 1.0 (the only value the generator
// emits today). When a future revision needs per-model
// temperatures, swap the literal for `{{printf "%#g" .Temperature}}`
// and accept the `%#g` default-precision output (e.g. "1.00000"
// for 1.0). Keeping the literal makes the generated files clean
// and the template trivially diff-able against the hand-curated
// precedent at x/catalog/models/doc.go.
//
// Note: the generator formats the result with go/format before
// writing, so whitespace inside the template is not significant.
const specTemplate = `// Code generated by cmd/modelsdev-gen. DO NOT EDIT.
//
// Source: https://models.dev (regenerated when the upstream catalog
// changes). Models here are the well-known [models.Spec] values for
// the {{.Family}} family. The Name field is the canonical wire
// name understood by the upstream primary's API.
//
// The catalog is generated; do not hand-edit these values. To add
// a new family, edit cmd/modelsdev-gen/main.go and re-run the
// generator.
package catalogmodels

import "github.com/andrewhowdencom/ore/models"

{{range .Specs}}
// {{.Identifier}} is the {{.Name}} model. {{if eq .Reasoning true}}It
// is a reasoning model; the spec's ThinkingLevel is set to medium
// so callers get reasoning by default.{{else}}The spec's
// ThinkingLevel is set to off; callers that want reasoning must
// override per-call.{{end}}
//
// Source: https://models.dev/api.json
var {{.Identifier}} = models.Spec{
	Name:            "{{.WireName}}",
	Window:          {{.Window}},
	MaxOutputTokens: {{.MaxOutput}},
	Temperature:     ptr(1.0),
	ThinkingLevel:   {{.ThinkingLevel}},
}
{{end}}
`

// templateData is the per-family payload rendered into the
// template above.
type templateData struct {
	Family  string
	Specs   []specData
}

// specData carries one rendered model. The fields are exported
// because the template uses them by name.
type specData struct {
	Identifier    string
	Name          string
	WireName      string
	Window        int
	MaxOutput     int
	ThinkingLevel string
	Reasoning     bool
}

// main is the binary entry point. Flags are minimal: a
// positional path overrides the output directory (default
// "x/catalog/models") and a -timeout flag bounds the network
// call. Any failure is fatal: the generator either regenerates
// the catalog or it leaves the previous commit alone.
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "modelsdev-gen: %v\n", err)
		os.Exit(1)
	}
}

// run is the testable entry point. The path to the output
// directory is the only knob; the upstream URL is a constant so
// all regenerations produce byte-identical files (modulo
// upstream drift).
func run() error {
	fs := flag.NewFlagSet("modelsdev-gen", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outDir := fs.String("out", "x/catalog/models", "output directory")
	timeout := fs.Duration("timeout", 30*time.Second, "HTTP timeout")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	// Print the header so the build log records what the
	// generator actually saw. Order: timestamp, URL, response
	// size. This makes a `git diff` on the generator output
	// easy to correlate with the upstream revision.
	fmt.Fprintf(os.Stderr, "modelsdev-gen: fetching %s\n", modelsDevURL)

	body, err := fetch(modelsDevURL, *timeout)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", modelsDevURL, err)
	}
	fmt.Fprintf(os.Stderr, "modelsdev-gen: %d bytes\n", len(body))

	var all map[string]modelsDevProvider
	if err := json.Unmarshal(body, &all); err != nil {
		return fmt.Errorf("decode models.dev: %w", err)
	}

	// Resolve the output directory relative to the current
	// working directory. The generator is run from the repo
	// root via `task generate`, so the default is the canonical
	// location of the catalog.
	abs, err := filepath.Abs(*outDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}

	tmpl, err := template.New("spec").Parse(specTemplate)
	if err != nil {
		return err
	}

	for _, provID := range primaryProviders {
		prov, ok := all[provID]
		if !ok {
			// Treat a missing allowlist entry as a hard
			// error: the operator added the vendor to
			// primaryProviders on the assumption models.dev
			// has it, and a silent skip would leave a
			// phantom file in the allowlist.
			return fmt.Errorf("primary provider %q not found in models.dev; remove it from primaryProviders or fix the URL", provID)
		}
		// Models are returned in a Go map (no insertion-order
		// guarantee). Sort by id to keep the generated file
		// stable across regenerations: any diff then reflects
		// upstream content changes, not Go's map iteration
		// order.
		ids := make([]string, 0, len(prov.Models))
		for id := range prov.Models {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		specs := make([]specData, 0, len(ids))
		for _, id := range ids {
			m := prov.Models[id]
			if m.Limit == nil {
				// Some upstream models omit the limit
				// block; we cannot emit a Spec without
				// a Window. Skip silently and log.
				fmt.Fprintf(os.Stderr, "modelsdev-gen: skip %s/%s (no limit)\n", provID, id)
				continue
			}
			specs = append(specs, toSpecData(m))
		}

		family := provID
		if alias, ok := providerAliases[provID]; ok {
			family = alias
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, templateData{Family: family, Specs: specs}); err != nil {
			return fmt.Errorf("render %s: %w", family, err)
		}
		formatted, err := format.Source(buf.Bytes())
		if err != nil {
			// A failure here is a generator bug: the
			// template emitted invalid Go. Surface the
			// raw body in the error so the bug is
			// debuggable.
			return fmt.Errorf("gofmt %s: %w\n--- raw ---\n%s", family, err, buf.String())
		}

		// File naming: one <family>.go per primary
		// provider. The family name is the provider id by
		// default, or the alias if one is configured.
		path := filepath.Join(abs, family+".go")
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "modelsdev-gen: wrote %s (%d models)\n", path, len(specs))
	}

	// Gateway lookup generation. The flat catalog is the
	// full models.dev response (not the primary allowlist):
	// the gateway id space is broader than the first-class
	// family list, so the join must consider every model
	// models.dev has indexed. Each gateway gets its own
	// lookup.go file under x/provider/<gateway>/.
	flatCatalog := buildFlatCatalog(all)
	if err := generateGatewayLookup(
		openRouterModelsURL,
		"OpenRouter",
		"openrouter",
		"x/provider/openrouter/lookup.go",
		flatCatalog,
		vendorPrefixesOpenRouter,
		*timeout,
	); err != nil {
		return err
	}
	if err := generateGatewayLookup(
		vercelModelsURL,
		"Vercel AI Gateway",
		"vercel",
		"x/provider/vercel/lookup.go",
		flatCatalog,
		vendorPrefixesVercel,
		*timeout,
	); err != nil {
		return err
	}

	return nil
}

// toSpecData maps one upstream model to the specData the template
// consumes. The Identifier is derived from the wire name (the id
// field) and is the Go-safe export that callers will reference
// (e.g. ClaudeOpus45). The Name is the human-readable upstream
// label; the WireName is the upstream wire id. The two are
// usually the same except where a provider's display name and
// model id diverge.
func toSpecData(m modelsDevModel) specData {
	level := "models.ThinkingLevelOff"
	if m.Reasoning {
		level = "models.ThinkingLevelMedium"
	}
	return specData{
		Identifier:    identifierFor(m.ID),
		Name:          m.Name,
		WireName:      m.ID,
		Window:        m.Limit.Context,
		MaxOutput:     m.Limit.Output,
		ThinkingLevel: level,
		Reasoning:     m.Reasoning,
	}
}

// identifierFor converts a wire name ("claude-opus-4-5",
// "gpt-4o-mini") into a Go-safe exported identifier
// ("ClaudeOpus45", "GPT4oMini"). The algorithm is: split on
// any non-alphanumeric rune, capitalize the first letter of
// each piece, drop everything else, prepend nothing (the Go
// convention is to drop the vendor prefix because the family
// is already implicit in the package name). Empty pieces are
// dropped.
//
// Examples:
//
//	"claude-opus-4-5"   -> "ClaudeOpus45"
//	"gpt-4o-mini"        -> "GPT4oMini"
//	"o1"                 -> "O1"
//	"MiniMax-M2.1"       -> "MiniMaxM21"
//	"claude-3.7-sonnet"  -> "Claude37Sonnet"
func identifierFor(wire string) string {
	// Split on any rune that is not a letter or digit. This
	// handles hyphens, dots, underscores, colons, slashes,
	// etc. in one pass.
	parts := strings.FieldsFunc(wire, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		// Capitalize the first rune; append the rest as-is.
		// Multi-rune first characters (e.g. "MiniMax") are
		// preserved verbatim — the algorithm only adjusts
		// the case of the first letter.
		first := []rune(p)[0]
		if first >= 'a' && first <= 'z' {
			first -= 'a' - 'A'
		}
		b.WriteRune(first)
		b.WriteString(p[utf8Len(first):])
	}
	return b.String()
}

// utf8Len returns the byte length of the first UTF-8-encoded
// rune in s. The caller guarantees s begins with a complete
// rune (it is the result of []rune conversion).
func utf8Len(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	default:
		return 4
	}
}

// fetch performs a single GET against url with a bounded
// timeout. The body is bounded only by the HTTP client's
// default behaviour; the upstream payload is on the order of
// a few megabytes so this is fine for a CLI tool.
func fetch(url string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// buildFlatCatalog flattens the primary-provider slice of the
// models.dev response into a single id->name map. The gateway
// join uses ONLY primary providers because the lookup is keyed
// by canonical [models.Spec.Name] values, which are by
// construction the primary provider ids. Non-primary providers
// (e.g. github-copilot) sometimes re-shape primary ids in
// gateway-specific ways (dots vs dashes) and including them
// in the flat catalog lets a non-canonical id displace its
// canonical sibling as a lookup key — e.g. github-copilot lists
// "claude-opus-4.5" while Anthropic lists "claude-opus-4-5".
// A "first write wins" map would pick one of the two
// non-deterministically; excluding non-primary providers
// removes the ambiguity entirely.
//
// The price of this restriction is that gateway models without
// a matching primary id are dropped from the lookup. That is
// the intended behaviour: a canonical name is the only thing
// the resolver translates from, so a gateway id that has no
// canonical counterpart is, by definition, unreachable through
// the resolver and does not belong in the lookup.
func buildFlatCatalog(all map[string]modelsDevProvider) map[string]string {
	out := map[string]string{}
	for _, provID := range primaryProviders {
		prov, ok := all[provID]
		if !ok {
			// Treat a missing allowlist entry as a hard
			// error: the operator added the vendor to
			// primaryProviders on the assumption models.dev
			// has it, and a silent skip would leave a
			// phantom file in the allowlist.
			panic(fmt.Sprintf("primary provider %q not found in models.dev; remove it from primaryProviders or fix the URL", provID))
		}
		for id, m := range prov.Models {
			if _, dup := out[id]; !dup {
				out[id] = m.Name
			}
		}
	}
	return out
}

// stripVendorPrefix attempts to remove a known vendor prefix
// from a gateway model id. Returns the empty string if no
// known prefix matches. The result is NOT normalized; callers
// that need both forms should call normalizeDots separately.
func stripVendorPrefix(id string, prefixes []string) string {
	for _, p := range prefixes {
		if strings.HasPrefix(id, p) {
			return strings.TrimPrefix(id, p)
		}
	}
	return ""
}

// normalizeDots replaces dots with dashes so a gateway id like
// "claude-opus-4.5" matches the dash form models.dev uses
// ("claude-opus-4-5"). Returns the input unchanged when no
// dots are present.
func normalizeDots(s string) string {
	return strings.ReplaceAll(s, ".", "-")
}

// isDateSuffix reports whether s is an 8-digit YYYYMMDD string.
// Used by joinCanonicalToGateway for the prefix-with-date fallback
// (e.g., the test entry "claude-opus-4-1" maps to OpenRouter's
// "anthropic/claude-opus-4.1" because models.dev's anthropic
// provider lists "claude-opus-4-1-20250805" with the same base id).
func isDateSuffix(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// joinCanonicalToGateway produces a sorted (canonical, gateway)
// slice by parsing each gateway model's vendor-prefixed id and
// matching the stripped remainder against the models.dev
// flat catalog. The first matching rule wins; the result is
// sorted by canonical name for stable generator output.
//
// Rules, in priority order:
//
//  1. Exact match on the stripped form (e.g. "gemini-2.5-pro").
//     Preserves the natural id as exposed by the upstream
//     gateway. This is the common case when the gateway and
//     models.dev agree on naming.
//
//  2. Exact match with dots normalized to dashes (e.g.
//     "claude-opus-4.5" -> "claude-opus-4-5"). Bridges the
//     convention difference between Anthropic (dots) and
//     models.dev (dashes) for the Opus 4.5 generation.
//
//  3. Date-suffix prefix match (e.g. "claude-opus-4-1" matches
//     "claude-opus-4-1-20250805" in models.dev). Handles the
//     case where models.dev lists a date-pinned variant but
//     the gateway uses the undated form.
//
// Gateway models without a recognized prefix, or whose stripped
// forms match no catalog entry under any rule, are dropped silently
// (logged via the generator's stderr summary).
func joinCanonicalToGateway(catalog map[string]string, models []gatewayModel, prefixes []string) []gatewayEntry {
	var out []gatewayEntry
	seen := map[string]bool{}
	for _, m := range models {
		stripped := stripVendorPrefix(m.ID, prefixes)
		if stripped == "" {
			continue
		}
		// Skip if either the natural or normalized form is
		// already emitted; this prevents the same gateway
		// model from producing two entries when it has
		// aliases upstream.
		normalized := normalizeDots(stripped)
		if seen[stripped] || seen[normalized] {
			continue
		}
		if canonical, ok := matchCanonical(catalog, stripped); ok {
			out = append(out, gatewayEntry{Canonical: canonical, Gateway: m.ID})
			seen[canonical] = true
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Canonical < out[j].Canonical
	})
	return out
}

// matchCanonical applies the three join rules in order against
// the catalog and returns the canonical key on the first hit.
// Returns ok=false when no rule matches; the caller skips the
// gateway model silently.
func matchCanonical(catalog map[string]string, stripped string) (string, bool) {
	// Rule 1: exact match on the natural form.
	if _, ok := catalog[stripped]; ok {
		return stripped, true
	}
	// Rule 2: exact match with dots normalized.
	normalized := normalizeDots(stripped)
	if normalized != stripped {
		if _, ok := catalog[normalized]; ok {
			return normalized, true
		}
	}
	// Rule 3: date-suffix prefix match. Try both the
	// natural and normalized forms; the first catalog entry
	// of the form "<candidate>-<8 digits>" wins.
	for _, candidate := range []string{stripped, normalized} {
		if candidate == "" {
			continue
		}
		for id := range catalog {
			if len(id) > len(candidate)+1 && strings.HasPrefix(id, candidate+"-") {
				suffix := id[len(candidate)+1:]
				if isDateSuffix(suffix) {
					return candidate, true
				}
			}
		}
	}
	return "", false
}

// generateGatewayLookup fetches one gateway catalog, joins it
// against the models.dev flat catalog, renders the lookup.go
// template, and writes it to outPath. The Package is the Go
// package name of the target directory (used in the file's
// `package <pkg>` line); Vendor is the human-readable gateway
// name (e.g., "OpenRouter", "Vercel AI Gateway") used in the
// file's docstring.
func generateGatewayLookup(
	sourceURL string,
	vendorName string,
	packageName string,
	outPath string,
	flatCatalog map[string]string,
	prefixes []string,
	timeout time.Duration,
) error {
	fmt.Fprintf(os.Stderr, "modelsdev-gen: fetching %s\n", sourceURL)
	body, err := fetch(sourceURL, timeout)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", sourceURL, err)
	}
	fmt.Fprintf(os.Stderr, "modelsdev-gen: %d bytes\n", len(body))

	var resp gatewayResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("decode %s: %w", vendorName, err)
	}
	fmt.Fprintf(os.Stderr, "modelsdev-gen: %s upstream has %d models\n", vendorName, len(resp.Data))

	entries := joinCanonicalToGateway(flatCatalog, resp.Data, prefixes)
	fmt.Fprintf(os.Stderr, "modelsdev-gen: %s joined %d entries\n", vendorName, len(entries))

	tmpl, err := template.New("lookup").Parse(gatewayLookupTemplate)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, gatewayTemplateData{
		SourceURL: sourceURL,
		Vendor:    vendorName,
		Package:   packageName,
		Entries:   entries,
	}); err != nil {
		return fmt.Errorf("render %s lookup: %w", vendorName, err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("gofmt %s lookup: %w\n--- raw ---\n%s", vendorName, err, buf.String())
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, formatted, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "modelsdev-gen: wrote %s (%d entries)\n", outPath, len(entries))
	return nil
}
