package session_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/session"
)

func TestInMemoryRegistry_RegisterAndGet(t *testing.T) {
	t.Parallel()

	registry := session.NewInMemoryRegistry()
	s := session.New("alpha", ledger.NewThread())
	t.Cleanup(func() { _ = s.Close() })

	if err := registry.Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := registry.Get("alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != s {
		t.Errorf("Get returned different pointer; expected the registered session")
	}
}

func TestInMemoryRegistry_DuplicateRegisterReturnsError(t *testing.T) {
	t.Parallel()

	registry := session.NewInMemoryRegistry()
	s := session.New("alpha", ledger.NewThread())
	t.Cleanup(func() { _ = s.Close() })

	if err := registry.Register(s); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Register a different session under the same ID; the existing
	// session must remain untouched.
	s2 := session.New("alpha", ledger.NewThread())
	t.Cleanup(func() { _ = s2.Close() })

	err := registry.Register(s2)
	if err == nil {
		t.Fatal("duplicate Register returned nil; expected ErrSessionAlreadyExists")
	}
	if !errors.Is(err, session.ErrSessionAlreadyExists) {
		t.Errorf("error %v does not wrap ErrSessionAlreadyExists", err)
	}

	got, err := registry.Get("alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != s {
		t.Errorf("duplicate Register replaced the original session")
	}
}

func TestInMemoryRegistry_GetMissingReturnsErrSessionNotFound(t *testing.T) {
	t.Parallel()

	registry := session.NewInMemoryRegistry()

	_, err := registry.Get("missing")
	if err == nil {
		t.Fatal("Get(missing) returned nil; expected ErrSessionNotFound")
	}
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Errorf("error %v does not wrap ErrSessionNotFound", err)
	}
}

func TestInMemoryRegistry_RemoveReturnsSession(t *testing.T) {
	t.Parallel()

	registry := session.NewInMemoryRegistry()
	s := session.New("alpha", ledger.NewThread())
	t.Cleanup(func() { _ = s.Close() })

	if err := registry.Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := registry.Remove("alpha")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got != s {
		t.Errorf("Remove returned different session; expected the registered one")
	}

	// After Remove, the session is no longer addressable.
	if _, err := registry.Get("alpha"); !errors.Is(err, session.ErrSessionNotFound) {
		t.Errorf("Get after Remove returned %v; expected ErrSessionNotFound", err)
	}
}

func TestInMemoryRegistry_RemoveMissingReturnsErrSessionNotFound(t *testing.T) {
	t.Parallel()

	registry := session.NewInMemoryRegistry()

	_, err := registry.Remove("missing")
	if err == nil {
		t.Fatal("Remove(missing) returned nil; expected ErrSessionNotFound")
	}
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Errorf("error %v does not wrap ErrSessionNotFound", err)
	}
}

func TestInMemoryRegistry_NilRegisterReturnsError(t *testing.T) {
	t.Parallel()

	registry := session.NewInMemoryRegistry()
	err := registry.Register(nil)
	if err == nil {
		t.Fatal("Register(nil) returned nil; expected an error")
	}
}

func TestInMemoryRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	registry := session.NewInMemoryRegistry()

	const writers = 8
	const readers = 8

	var wg sync.WaitGroup

	// Concurrent writers register disjoint IDs.
	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "writer-" + itoa(i)
			s := session.New(id, ledger.NewThread())
			t.Cleanup(func() { _ = s.Close() })
			if err := registry.Register(s); err != nil {
				t.Errorf("Register(%s): %v", id, err)
			}
		}()
	}

	// Concurrent readers perform Get for each ID.
	for i := 0; i < readers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "writer-" + itoa(i)
			// The writer may not have finished yet; tolerate either
			// a hit (session present) or an ErrSessionNotFound.
			_, err := registry.Get(id)
			if err != nil && !errors.Is(err, session.ErrSessionNotFound) {
				t.Errorf("Get(%s) returned unexpected error: %v", id, err)
			}
		}()
	}

	wg.Wait()

	// All writers must have successfully registered.
	for i := 0; i < writers; i++ {
		if _, err := registry.Get("writer-" + itoa(i)); err != nil {
			t.Errorf("after concurrent writers, Get(writer-%d): %v", i, err)
		}
	}
}

// itoa is a small helper that avoids pulling in strconv just for
// integer-to-string in the concurrent access test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Compile-time assertion that *InMemoryRegistry satisfies Registry.
var _ session.Registry = (*session.InMemoryRegistry)(nil)