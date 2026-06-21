package cognitive

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/verifier"
)

// VerificationOption configures the WithVerification wrapper.
type VerificationOption func(*verifyingPattern)

// WithVerifiers sets the verifiers to run after the inner pattern completes.
func WithVerifiers(v ...verifier.Verifier) VerificationOption {
	return func(p *verifyingPattern) {
		p.verifiers = append(p.verifiers, v...)
	}
}

// WithMaxRetries sets the maximum number of retries on verification failure.
// Default is 3.
func WithMaxRetries(n int) VerificationOption {
	return func(p *verifyingPattern) {
		p.maxRetries = n
	}
}

// WithVerification wraps a Pattern and runs verifiers after each completion.
// If verifiers fail, it injects a system turn with the combined report and
// retries the inner pattern up to maxRetries times. If any verifier returns
// an Error status, the error is propagated immediately as a fatal error.
func WithVerification(inner Pattern, step loop.TurnSubmitter, opts ...VerificationOption) Pattern {
	p := &verifyingPattern{
		inner:      inner,
		submitter:  step,
		maxRetries: 3,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type verifyingPattern struct {
	inner      Pattern
	submitter  loop.TurnSubmitter
	verifiers  []verifier.Verifier
	maxRetries int
}

func (p *verifyingPattern) Run(ctx context.Context, st state.State) (state.State, error) {
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		result, err := p.inner.Run(ctx, st)
		if err != nil {
			return result, err
		}

		results := verifier.RunAll(ctx, p.verifiers, result)

		hasError := false
		hasFail := false
		for _, r := range results {
			if r.Status == verifier.VerificationError {
				hasError = true
				break
			}
			if r.Status == verifier.VerificationFail {
				hasFail = true
			}
		}

		if hasError {
			report := verifier.BuildReport(results)
			return result, fmt.Errorf("verification error: %s", report)
		}

		if !hasFail {
			return result, nil
		}

		if attempt >= p.maxRetries {
			report := verifier.BuildReport(results)
			return result, fmt.Errorf("verification failed after %d retries: %s", p.maxRetries, report)
		}

		report := verifier.BuildReport(results)
		_, err = p.submitter.Submit(ctx, result, state.RoleSystem, artifact.Text{Content: report})
		if err != nil {
			return result, fmt.Errorf("failed to inject verification report: %w", err)
		}

		st = result
	}

	return st, fmt.Errorf("verification loop exhausted")
}

// Compile-time assertion that verifyingPattern implements Pattern.
var _ Pattern = (*verifyingPattern)(nil)

// Name returns the pattern identifier, used by the agent bundle for
// tracing the agent.run span. Stable across versions.
func (p *verifyingPattern) Name() string { return "verified" }
