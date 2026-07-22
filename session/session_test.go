package session_test

import (
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
)

func TestNew_ConstructsAndRuns(t *testing.T) {
	t.Parallel()

	thread := ledger.NewThread()
	s := session.New("test-id", thread)
	defer s.Close()

	if s.ID() != "test-id" {
		t.Fatalf("ID() = %q, want %q", s.ID(), "test-id")
	}
	if s.Thread() != thread {
		t.Fatal("Thread() did not return the thread passed to New")
	}
}

func TestSetMetadata_EmitsPropertiesEvent(t *testing.T) {
	t.Parallel()

	thread := ledger.NewThread()
	s := session.New("test-id", thread)
	defer s.Close()

	events := s.Subscribe("properties")

	s.SetMetadata("test.key", "test.value")
	select {
	case evt := <-events:
		pe, ok := evt.(loop.PropertiesEvent)
		if !ok {
			t.Fatalf("expected PropertiesEvent, got %T", evt)
		}
		var got string
		var present bool
		for _, op := range pe.Operations {
			if op.Op == loop.PropertyOpSet && op.Key == "test.key" {
				got, present = op.Value, true
				break
			}
		}
		if !present {
			t.Fatalf("expected set op for test.key, got ops=%+v", pe.Operations)
		}
		if got != "test.value" {
			t.Fatalf("expected property test.key=test.value, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for PropertiesEvent")
	}
}

func TestGetMetadata_RoundTrips(t *testing.T) {
	t.Parallel()

	thread := ledger.NewThread()
	s := session.New("test-id", thread)
	defer s.Close()

	s.SetMetadata("a", "1")
	s.SetMetadata("b", "2")

	if got, ok := s.GetMetadata("a"); !ok || got != "1" {
		t.Fatalf("GetMetadata(a) = (%q, %v), want (\"1\", true)", got, ok)
	}
	if got, ok := s.GetMetadata("b"); !ok || got != "2" {
		t.Fatalf("GetMetadata(b) = (%q, %v), want (\"2\", true)", got, ok)
	}
	if _, ok := s.GetMetadata("missing"); ok {
		t.Fatal("GetMetadata(missing) returned ok=true")
	}

	all := s.AllMetadata()
	if all["a"] != "1" || all["b"] != "2" {
		t.Fatalf("AllMetadata() = %+v, want {a:1, b:2}", all)
	}
}

func TestSubscribe_AfterCloseReturnsClosedChannel(t *testing.T) {
	t.Parallel()

	thread := ledger.NewThread()
	s := session.New("test-id", thread)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ch := s.Subscribe("lifecycle")
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("Subscribe after Close yielded an event")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Subscribe after Close did not return a closed channel")
	}
}

func TestClose_Idempotent(t *testing.T) {
	t.Parallel()

	thread := ledger.NewThread()
	s := session.New("test-id", thread)

	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
