package stdio

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestNew_NilSession(t *testing.T) {
	c, err := New(nil)
	require.Error(t, err)
	require.Nil(t, c)
}

// TestNew_Defaults asserts the constructor binds to the supplied
// session and uses the os.* defaults for io.Reader / io.Writer.
func TestNew_Defaults(t *testing.T) {
	sess := session.New("test-thread", ledger.NewThread())
	c, err := New(sess)
	require.NoError(t, err)
	require.NotNil(t, c)

	s, ok := c.(*stdio)
	require.True(t, ok)
	require.Same(t, sess, s.sess)
	require.Same(t, os.Stdin, s.in)
	require.Same(t, os.Stdout, s.out)
	require.Same(t, os.Stderr, s.err)
	require.Nil(t, s.tracer)
}

func TestNew_WithInput(t *testing.T) {
	sess := session.New("test-thread", ledger.NewThread())
	in := newBytesReader("hello")
	c, err := New(sess, WithInput(in))
	require.NoError(t, err)
	s := c.(*stdio)
	require.Same(t, in, s.in)
}

func TestNew_WithOutput(t *testing.T) {
	sess := session.New("test-thread", ledger.NewThread())
	out := newDiscardWriter()
	c, err := New(sess, WithOutput(out))
	require.NoError(t, err)
	s := c.(*stdio)
	require.Same(t, out, s.out)
}

func TestNew_WithStderr(t *testing.T) {
	sess := session.New("test-thread", ledger.NewThread())
	errOut := newDiscardWriter()
	c, err := New(sess, WithStderr(errOut))
	require.NoError(t, err)
	s := c.(*stdio)
	require.Same(t, errOut, s.err)
}

func TestNew_WithTracer(t *testing.T) {
	sess := session.New("test-thread", ledger.NewThread())
	tr := trace.NewNoopTracerProvider().Tracer("")
	c, err := New(sess, WithTracer(tr))
	require.NoError(t, err)
	s := c.(*stdio)
	require.Equal(t, tr, s.tracer)
}

// TestDescriptor covers the package-level Descriptor. The stdio
// conduit advertises its capabilities for the conduit registry.
func TestDescriptor(t *testing.T) {
	require.Equal(t, "stdio", Descriptor.Name)
	require.NotEmpty(t, Descriptor.Description)
}

// TestDescriptor_Capabilities enumerates the capabilities the
// stdio conduit advertises. If this list grows, downstream
// tooling that introspects capabilities will see the new keys;
// keep this list in sync with the implementation in Start().
func TestDescriptor_Capabilities(t *testing.T) {
	caps := make(map[conduit.Capability]bool)
	for _, c := range Descriptor.Capabilities {
		caps[c] = true
	}
	require.True(t, caps[conduit.CapEventSource], "stdio emits events (user messages)")
	require.True(t, caps[conduit.CapRenderMarkdown], "stdio renders assistant markdown")
	require.True(t, caps[conduit.CapAcceptText], "stdio accepts a text payload")
}

// newBytesReader / newDiscardWriter are local helpers that avoid
// pulling io-test's full machinery into the package's test
// dependencies.
func newBytesReader(s string) *byteReader     { return &byteReader{s: s} }
func newDiscardWriter() *discardWriter     { return &discardWriter{} }

type byteReader struct {
	s string
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// Compile-time checks: the helpers satisfy the io interfaces.
var _ io.Reader = (*byteReader)(nil)
var _ io.Writer = (*discardWriter)(nil)

// Ensure context import is used even if not directly referenced.
var _ = errors.New