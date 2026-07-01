package verifier

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
)

// Pattern is the shape of a runnable, retryable state transformer that
// WithVerification accepts and produces. Anything implementing the two
// methods below satisfies the contract — including *cognitive.ReAct and
// *cognitive.SingleShot. The wrapper returned by WithVerification itself
// also implements Pattern, so verifiers may be composed recursively.
type Pattern interface {
	Run(ctx context.Context, st ledger.State) (ledger.State, error)
	Name() string
}

// Option configures the wrapper produced by WithVerification.
type Option func(*verifyingPattern)

// WithVerifiers sets the verifiers to run after the inner pattern completes.
func WithVerifiers(v ...Verifier) Option {
	return func(p *verifyingPattern) {
		p.verifiers = append(p.verifiers, v...)
	}
}

// WithMaxRetries sets the maximum number of retries on verification failure.
// Default is 3.
func WithMaxRetries(n int) Option {
	return func(p *verifyingPattern) {
		p.maxRetries = n
	}
}

// WithVerification wraps a Pattern and runs verifiers after each completion.
// If verifiers fail, it injects a system turn with the combined report and
// retries the inner pattern up to maxRetries times. If any verifier returns
// an Error status, the error is propagated immediately as a fatal error.
func WithVerification(inner Pattern, step loop.TurnSubmitter, opts ...Option) Pattern {
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
	verifiers  []Verifier
	maxRetries int
}

func (p *verifyingPattern) Run(ctx context.Context, st ledger.State) (ledger.State, error) {
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		result, err := p.inner.Run(ctx, st)
		if err != nil {
			return result, err
		}

		results := RunAll(ctx, p.verifiers, result)

		hasError := false
		hasFail := false
		for _, r := range results {
			if r.Status == VerificationError {
				hasError = true
				break
			}
			if r.Status == VerificationFail {
				hasFail = true
			}
		}

		if hasError {
			report := BuildReport(results)
			return result, fmt.Errorf("verification error: %s", report)
		}

		if !hasFail {
			return result, nil
		}

		if attempt >= p.maxRetries {
			report := BuildReport(results)
			return result, fmt.Errorf("verification failed after %d retries: %s", p.maxRetries, report)
		}

		report := BuildReport(results)
		_, err = p.submitter.Submit(ctx, result, ledger.RoleSystem, artifact.Text{Content: report})
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
