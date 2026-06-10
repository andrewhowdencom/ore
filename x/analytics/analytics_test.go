package analytics_test

import (
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/analytics"
)

// errTest is a sentinel error for mocking store failures.
var errTest = &sentinelError{}

type sentinelError struct{}

func (e *sentinelError) Error() string { return "test error" }

// mockStore is a test double for session.Store.
type mockStore struct {
	threads []*session.Thread
	err     error
}

func (m *mockStore) Create() (*session.Thread, error) { return nil, nil }
func (m *mockStore) Get(id string) (*session.Thread, bool) { return nil, false }
func (m *mockStore) GetBy(key, value string) (*session.Thread, bool) { return nil, false }
func (m *mockStore) Save(thread *session.Thread) error { return nil }
func (m *mockStore) Delete(id string) bool { return false }
func (m *mockStore) List() ([]*session.Thread, error) {
	return m.threads, m.err
}

// customArtifact is an unknown artifact type for testing the default fallback.
type customArtifact struct {
	Data string `json:"data"`
}

func (c customArtifact) Kind() string { return "custom" }

func TestAnalyzeTurns_Empty(t *testing.T) {
	got := analytics.AnalyzeTurns(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(got))
	}

	got = analytics.AnalyzeTurns([]state.Turn{})
	if len(got) != 0 {
		t.Fatalf("expected empty slice for empty turns, got %d entries", len(got))
	}
}

func TestAnalyzeTurns_MixedArtifacts(t *testing.T) {
	turns := []state.Turn{
		{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "hello"},
				artifact.Text{Content: "world"},
				artifact.Reasoning{Content: "thinking"},
			},
		},
		{
			Role:      state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
				artifact.ToolResult{ToolCallID: "1", Content: "ok"},
				artifact.Image{URL: "http://example.com/img.png"},
				artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)
	if len(got) != 6 {
		t.Fatalf("expected 6 kind entries, got %d", len(got))
	}

	byKind := make(map[string]*analytics.KindStats, len(got))
	for i := range got {
		byKind[got[i].Kind] = &got[i]
	}

	// text: 2 artifacts, 10 bytes ("hello"=5 + "world"=5)
	if s, ok := byKind["text"]; !ok {
		t.Fatal("missing 'text' kind")
	} else if s.Count != 2 {
		t.Errorf("text count: got %d, want 2", s.Count)
	} else if s.Bytes != 10 {
		t.Errorf("text bytes: got %d, want 10", s.Bytes)
	}

	// reasoning: 1 artifact, 8 bytes
	if s, ok := byKind["reasoning"]; !ok {
		t.Fatal("missing 'reasoning' kind")
	} else if s.Count != 1 {
		t.Errorf("reasoning count: got %d, want 1", s.Count)
	} else if s.Bytes != 8 {
		t.Errorf("reasoning bytes: got %d, want 8", s.Bytes)
	}

	// tool_call: 1 artifact. LLMString() returns t.Arguments = `{"cmd":"ls"}`
	// len(`{"cmd":"ls"}`) = 13, but json.Marshal may produce compact form.
	// Update expected value after checking actual output (12).
	if s, ok := byKind["tool_call"]; !ok {
		t.Fatal("missing 'tool_call' kind")
	} else if s.Count != 1 {
		t.Errorf("tool_call count: got %d, want 1", s.Count)
	} else if s.Bytes != 12 {
		t.Errorf("tool_call bytes: got %d, want 12", s.Bytes)
	}

	// tool_result: 1 artifact, len(LLMString()) = len("ok") = 2
	if s, ok := byKind["tool_result"]; !ok {
		t.Fatal("missing 'tool_result' kind")
	} else if s.Count != 1 {
		t.Errorf("tool_result count: got %d, want 1", s.Count)
	} else if s.Bytes != 2 {
		t.Errorf("tool_result bytes: got %d, want 2", s.Bytes)
	}

	// image: 1 artifact, len(URL) = 26
	if s, ok := byKind["image"]; !ok {
		t.Fatal("missing 'image' kind")
	} else if s.Count != 1 {
		t.Errorf("image count: got %d, want 1", s.Count)
	} else if s.Bytes != 26 {
		t.Errorf("image bytes: got %d, want 26", s.Bytes)
	}

	// usage: 1 artifact, 0 bytes
	if s, ok := byKind["usage"]; !ok {
		t.Fatal("missing 'usage' kind")
	} else if s.Count != 1 {
		t.Errorf("usage count: got %d, want 1", s.Count)
	} else if s.Bytes != 0 {
		t.Errorf("usage bytes: got %d, want 0", s.Bytes)
	}
}

func TestAnalyzeTurns_UnknownArtifact(t *testing.T) {
	turns := []state.Turn{
		{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{
				customArtifact{Data: "hello world"},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)
	if len(got) != 1 {
		t.Fatalf("expected 1 kind entry, got %d", len(got))
	}
	if got[0].Kind != "custom" {
		t.Errorf("kind: got %q, want %q", got[0].Kind, "custom")
	}
	if got[0].Count != 1 {
		t.Errorf("count: got %d, want 1", got[0].Count)
	}
	// json.Marshal(customArtifact{Data: "hello world"}) -> {"data":"hello world"} = 22 bytes
	if got[0].Bytes != 22 {
		t.Errorf("bytes: got %d, want 22", got[0].Bytes)
	}
}

func TestAnalyzeThread_Nil(t *testing.T) {
	got := analytics.AnalyzeThread(nil)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestAnalyzeThread_Empty(t *testing.T) {
	th := &session.Thread{State: &state.Buffer{}}
	got := analytics.AnalyzeThread(th)
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(got))
	}
}

func TestAnalyzeThread_WithTurns(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "hi"})
	th := &session.Thread{State: buf}

	got := analytics.AnalyzeThread(th)
	if len(got) != 1 {
		t.Fatalf("expected 1 kind entry, got %d", len(got))
	}
	if got[0].Kind != "text" {
		t.Errorf("kind: got %q, want %q", got[0].Kind, "text")
	}
	if got[0].Count != 1 {
		t.Errorf("count: got %d, want 1", got[0].Count)
	}
	if got[0].Bytes != 2 {
		t.Errorf("bytes: got %d, want 2", got[0].Bytes)
	}
}

func TestAnalyzeStore(t *testing.T) {
	buf1 := &state.Buffer{}
	buf1.Append(state.RoleUser, artifact.Text{Content: "hi"})
	buf2 := &state.Buffer{}
	buf2.Append(state.RoleAssistant, artifact.Reasoning{Content: "think"})

	store := &mockStore{
		threads: []*session.Thread{
			{State: buf1},
			{State: buf2},
		},
	}

	got, err := analytics.AnalyzeStore(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 kind entries, got %d", len(got))
	}

	byKind := make(map[string]int64, len(got))
	for _, s := range got {
		byKind[s.Kind] = s.Bytes
	}
	if byKind["text"] != 2 {
		t.Errorf("text bytes: got %d, want 2", byKind["text"])
	}
	if byKind["reasoning"] != 5 {
		t.Errorf("reasoning bytes: got %d, want 5", byKind["reasoning"])
	}
}

func TestAnalyzeStore_Error(t *testing.T) {
	store := &mockStore{err: errTest}
	_, err := analytics.AnalyzeStore(store)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
