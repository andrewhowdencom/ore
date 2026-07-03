package session_test

import (
	"context"
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

func TestRun_EnqueuesAndEmitsLifecycleDone(t *testing.T) {
	t.Parallel()

	thread := ledger.NewThread()
	s := session.New("test-id", thread)
	defer s.Close()

	events := s.Subscribe("lifecycle")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.Run(ctx, session.UserMessageEvent{
		Content: "hello",
		Ctx:     ctx,
	}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	deadline := time.After(2 * time.Second)
	got := 0
	for got < 1 {
		select {
		case evt, ok := <-events:
			if !ok {
				t.Fatalf("event channel closed early; got %d events", got)
			}
			le, isLifecycle := evt.(loop.LifecycleEvent)
			if !isLifecycle {
				t.Fatalf("expected LifecycleEvent, got %T", evt)
			}
			if le.Phase != "done" {
				t.Fatalf("expected Phase=done, got %q", le.Phase)
			}
			got++
		case <-deadline:
			t.Fatalf("timeout waiting for LifecycleEvent; got %d events", got)
		}
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
		if pe.Properties["test.key"] != "test.value" {
			t.Fatalf("expected property test.key=test.value, got %+v", pe.Properties)
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

func TestRun_AfterCloseReturnsError(t *testing.T) {
	t.Parallel()

	thread := ledger.NewThread()
	s := session.New("test-id", thread)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := s.Run(context.Background(), session.UserMessageEvent{Content: "x"})
	if err == nil {
		t.Fatal("Run after Close returned nil; expected error")
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