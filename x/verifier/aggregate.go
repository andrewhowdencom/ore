package verifier

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/andrewhowdencom/ore/state"
)

// RunAll runs all verifiers in parallel against the given state and returns
// a slice of VerificationResult. The order of results is sorted by name
// for determinism. Each verifier runs in its own goroutine; the function
// waits for all to complete before returning.
func RunAll(ctx context.Context, verifiers []Verifier, st state.State) []VerificationResult {
	var wg sync.WaitGroup
	results := make([]VerificationResult, len(verifiers))

	for i, v := range verifiers {
		wg.Add(1)
		go func(idx int, ver Verifier) {
			defer wg.Done()
			res, err := ver.Verify(ctx, st)
			if err != nil {
				res.Status = VerificationError
				if res.Report == "" {
					res.Report = err.Error()
				}
			}
			results[idx] = res
		}(i, v)
	}

	wg.Wait()

	// Sort by name for determinism.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results
}

// BuildReport formats a slice of VerificationResult as a markdown report.
func BuildReport(results []VerificationResult) string {
	var b strings.Builder
	b.WriteString("# Verification Report\n\n")

	for _, r := range results {
		b.WriteString(fmt.Sprintf("## %s\n\n", r.Name))
		b.WriteString(fmt.Sprintf("**Status:** %s\n\n", r.Status.String()))
		if r.Report != "" {
			b.WriteString("```\n")
			b.WriteString(r.Report)
			if !strings.HasSuffix(r.Report, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n")
		}
	}

	return b.String()
}
