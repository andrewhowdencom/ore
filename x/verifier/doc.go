// Package verifier defines quality gates that can run after a cognitive
// pattern completes. Verifiers inspect the conversation state (or the
// external world) and return a pass/fail/error result. Multiple verifiers
// can be composed into an aggregate report.
//
// The core abstractions:
//
//   - Verifier — Verify(ctx, ledger.State) → (VerificationResult, error)
//
//   - Pattern — Run(ctx, ledger.State) → (ledger.State, error); any
//     implementation that satisfies this interface (e.g. *cognitive.ReAct,
//     *cognitive.SingleShot) can be wrapped with WithVerification.
//
// Concrete implementations include:
//
//   - ExecVerifier — runs a shell command and maps exit codes to Status.
//
// The Aggregate function runs multiple verifiers in parallel and produces
// a combined markdown report.
//
// WithVerification wraps any Pattern. After each successful inner run, it
// runs the configured Verifiers and, on failure, injects a system turn
// containing the combined markdown report and retries the inner pattern up
// to a configurable limit. It depends on loop.TurnSubmitter to inject the
// system turn.
package verifier
