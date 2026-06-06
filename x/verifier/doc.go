// Package verifier defines quality gates that can run after a cognitive
// pattern completes. Verifiers inspect the conversation state (or the
// external world) and return a pass/fail/error result. Multiple verifiers
// can be composed into an aggregate report.
//
// The core abstraction is the Verifier interface:
//
//	Verify(ctx, state.State) → (VerificationResult, error)
//
// Concrete implementations include:
//
//   - ExecVerifier — runs a shell command and maps exit codes to Status.
//
// The Aggregate function runs multiple verifiers in parallel and produces
// a combined markdown report.
package verifier
