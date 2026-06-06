package verifier

import (
	"context"

	"github.com/andrewhowdencom/ore/state"
)

// Status represents the outcome of a single verification.
type Status int

const (
	VerificationPass Status = iota
	VerificationFail
	VerificationError
)

// String returns a human-readable status name.
func (s Status) String() string {
	switch s {
	case VerificationPass:
		return "Pass"
	case VerificationFail:
		return "Fail"
	case VerificationError:
		return "Error"
	default:
		return "Unknown"
	}
}

// VerificationResult is the outcome of a single verifier.
type VerificationResult struct {
	Name   string
	Status Status
	Report string
}

// Verifier runs a quality gate against the conversation state or the
// external environment (e.g., compilation, tests, lint).
type Verifier interface {
	Verify(ctx context.Context, st state.State) (VerificationResult, error)
}
