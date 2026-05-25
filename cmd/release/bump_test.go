package main

import "testing"

func TestIsBreaking(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"feat!: add feature", true},
		{"feat(scope)!: add feature", true},
		{"fix!: bugfix", true},
		{"chore(deps)!: update", true},
		{"fix: bug\n\nBREAKING CHANGE: api change", true},
		{"fix: bug\n\nBREAKING-CHANGE: api change", true},
		{"fix: bug", false},
		{"feat: add feature", false},
		{"fix(weird!scope): bug", false},
		{"some random message", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			if got := isBreaking(tt.msg); got != tt.want {
				t.Errorf("isBreaking(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestIsFeat(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"feat: add feature", true},
		{"feat(scope): add feature", true},
		{"feat!: add feature", true}, // isBreaking handles the !, but isFeat should still recognise it
		{"feat(scope)!: add feature", true},
		{"feature: add something", false},
		{"feats: add something", false},
		{"fix: bug", false},
		{"chore: cleanup", false},
		{"some random message", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			if got := isFeat(tt.msg); got != tt.want {
				t.Errorf("isFeat(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestBumpType(t *testing.T) {
	tests := []struct {
		name string
		msgs []string
		want Bump
	}{
		{"empty", nil, None},
		{"single fix", []string{"fix: bug"}, Patch},
		{"single feat", []string{"feat: add feature"}, Minor},
		{"single breaking", []string{"feat!: breaking"}, Major},
		{"fix then feat", []string{"fix: bug", "feat: add feature"}, Minor},
		{"fix then breaking", []string{"fix: bug", "feat!: breaking"}, Major},
		{"non-conventional", []string{"random message"}, Patch},
		{"docs commit", []string{"docs: update readme"}, Patch},
		{"multiple breaking", []string{"fix: bug", "feat: feature", "chore!: break"}, Major},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bumpType(tt.msgs); got != tt.want {
				t.Errorf("bumpType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextVersion(t *testing.T) {
	tests := []struct {
		current string
		bump    Bump
		want    string
		wantErr bool
	}{
		{"", Patch, "v0.1.0", false},
		{"", Minor, "v0.1.0", false},
		{"", Major, "v0.1.0", false},
		{"v0.1.0", Patch, "v0.1.1", false},
		{"v0.1.0", Minor, "v0.2.0", false},
		{"v0.1.0", Major, "v1.0.0", false},
		{"v1.2.3", Patch, "v1.2.4", false},
		{"v1.2.3", Minor, "v1.3.0", false},
		{"v1.2.3", Major, "v2.0.0", false},
		{"v0.0.0-00010101000000-000000000000", Patch, "v0.0.1", false},
		{"invalid", Patch, "", true},
		{"0.0.1", Patch, "", true},
		{"v0.1.0", None, "v0.1.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.current+"_"+tt.bump.String(), func(t *testing.T) {
			got, err := nextVersion(tt.current, tt.bump)
			if (err != nil) != tt.wantErr {
				t.Fatalf("nextVersion(%q, %v) error = %v, wantErr %v", tt.current, tt.bump, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("nextVersion(%q, %v) = %q, want %q", tt.current, tt.bump, got, tt.want)
			}
		})
	}
}
