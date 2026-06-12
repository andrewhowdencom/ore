package tool

import (
	"testing"
)

func TestFormat_ResolvedTruncateConfig_ZeroFallsBackToDefaults(t *testing.T) {
	t.Parallel()

	var f Format
	got := f.resolvedTruncateConfig()

	if got.MaxBytes != FrameworkDefaultMaxBytes {
		t.Errorf("MaxBytes = %d, want %d", got.MaxBytes, FrameworkDefaultMaxBytes)
	}
	if got.MaxLines != FrameworkDefaultMaxLines {
		t.Errorf("MaxLines = %d, want %d", got.MaxLines, FrameworkDefaultMaxLines)
	}
}

func TestFormat_ResolvedTruncateConfig_PartialOverride(t *testing.T) {
	t.Parallel()

	f := Format{Truncate: TruncateConfig{MaxBytes: 100}}
	got := f.resolvedTruncateConfig()

	if got.MaxBytes != 100 {
		t.Errorf("MaxBytes = %d, want 100", got.MaxBytes)
	}
	if got.MaxLines != FrameworkDefaultMaxLines {
		t.Errorf("MaxLines = %d, want %d (default)", got.MaxLines, FrameworkDefaultMaxLines)
	}
}

func TestFormat_ResolvedTruncateConfig_ExplicitZeroMaxLinesUsesDefault(t *testing.T) {
	t.Parallel()

	// MaxLines: 0 means "use default"; only positive values override.
	f := Format{Truncate: TruncateConfig{MaxBytes: 200, MaxLines: 0}}
	got := f.resolvedTruncateConfig()

	if got.MaxBytes != 200 {
		t.Errorf("MaxBytes = %d, want 200", got.MaxBytes)
	}
	if got.MaxLines != FrameworkDefaultMaxLines {
		t.Errorf("MaxLines = %d, want %d (default because 0 is zero-value)", got.MaxLines, FrameworkDefaultMaxLines)
	}
}

func TestFormat_ResolvedTruncateConfig_FullOverride(t *testing.T) {
	t.Parallel()

	f := Format{Truncate: TruncateConfig{MaxBytes: 10, MaxLines: 5}}
	got := f.resolvedTruncateConfig()

	if got.MaxBytes != 10 {
		t.Errorf("MaxBytes = %d, want 10", got.MaxBytes)
	}
	if got.MaxLines != 5 {
		t.Errorf("MaxLines = %d, want 5", got.MaxLines)
	}
}

func TestTruncationStyle_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    TruncationStyle
		want string
	}{
		{"zero value (tail)", StyleTail, "tail"},
		{"head", StyleHead, "head"},
		{"unknown", TruncationStyle(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultTruncateConfig(t *testing.T) {
	t.Parallel()

	got := DefaultTruncateConfig()
	if got.MaxBytes != FrameworkDefaultMaxBytes {
		t.Errorf("MaxBytes = %d, want %d", got.MaxBytes, FrameworkDefaultMaxBytes)
	}
	if got.MaxLines != FrameworkDefaultMaxLines {
		t.Errorf("MaxLines = %d, want %d", got.MaxLines, FrameworkDefaultMaxLines)
	}
}
