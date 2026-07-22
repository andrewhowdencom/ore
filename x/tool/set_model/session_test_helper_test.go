package set_model

import (
	"testing"

	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/session"
	"github.com/stretchr/testify/require"
)

// newMockSession constructs a real *session.Session backed by an empty
// ledger thread. The slash handler only needs the session's GetMetadata /
// SetMetadata methods; session.New requires no provider, processor, or
// store.
func newMockSession(t *testing.T) *session.Session {
	t.Helper()

	thread := ledger.NewThread()
	s := session.New("test-id", thread)
	require.NotNil(t, s)
	return s
}