package artifact

import (
	"encoding/json"
	"testing"
)

func TestTruncation_Truncated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		t    *Truncation
		want bool
	}{
		{"nil receiver", nil, false},
		{"zero value", &Truncation{}, false},
		{"equal bytes (no truncation)", &Truncation{OriginalBytes: 100, ShownBytes: 100}, false},
		{"truncated", &Truncation{OriginalBytes: 1000, ShownBytes: 50}, true},
		{"truncated by one byte", &Truncation{OriginalBytes: 101, ShownBytes: 100}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.t.Truncated(); got != tt.want {
				t.Errorf("Truncated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncation_MarshalJSON_OmitsEmpty(t *testing.T) {
	t.Parallel()

	// Empty Truncation should still marshal; "omitempty" is on the
	// individual fields, not the whole struct.
	got, err := json.Marshal(Truncation{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// The ShownBytes and ShownLines fields don't have omitempty
	// (they are always present), so they should appear as 0.
	// OriginalBytes/OriginalLines/Style/RecoveryHint have omitempty
	// and should not appear.
	if got := string(got); !contains(got, `"shown_bytes":0`) {
		t.Errorf("expected shown_bytes:0 in %s", got)
	}
	if !contains(string(got), `"shown_lines":0`) {
		t.Errorf("expected shown_lines:0 in %s", got)
	}
	if contains(string(got), `"original_bytes"`) {
		t.Errorf("original_bytes should be omitted, got %s", got)
	}
	if contains(string(got), `"recovery_hint"`) {
		t.Errorf("recovery_hint should be omitted, got %s", got)
	}
}

func TestToolResult_MarshalJSON_IncludesTruncation(t *testing.T) {
	t.Parallel()

	tr := &Truncation{
		OriginalBytes: 1000,
		OriginalLines: 100,
		ShownBytes:    50,
		ShownLines:    5,
		Style:         "tail",
	}

	r := ToolResult{
		ToolCallID: "call_1",
		Content:    "truncated content",
		IsError:    false,
		Truncation: tr,
	}

	got, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	s := string(got)
	if !contains(s, `"truncation"`) {
		t.Errorf("expected truncation field in %s", s)
	}
	if !contains(s, `"original_bytes":1000`) {
		t.Errorf("expected original_bytes:1000 in %s", s)
	}
	if !contains(s, `"style":"tail"`) {
		t.Errorf("expected style:tail in %s", s)
	}
}

func TestToolResult_MarshalJSON_OmitsNilTruncation(t *testing.T) {
	t.Parallel()

	r := ToolResult{
		ToolCallID: "call_1",
		Content:    "small",
	}

	got, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	s := string(got)
	if contains(s, `"truncation"`) {
		t.Errorf("truncation should be omitted when nil, got %s", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
