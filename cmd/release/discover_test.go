package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseModulePath(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "simple module",
			content: "module github.com/example/foo\n\ngo 1.21\n",
			want:    "github.com/example/foo",
		},
		{
			name:    "module with trailing comment",
			content: "module github.com/example/bar // comment\n",
			want:    "github.com/example/bar",
		},
		{
			name:    "no module directive",
			content: "go 1.21\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "go.mod")
			if err := os.WriteFile(f, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}
			got, err := parseModulePath(f)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseModulePath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseModulePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiscoverModules(t *testing.T) {
	root := t.TempDir()

	// Root module
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/example/root\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Submodule
	subDir := filepath.Join(root, "x", "provider", "openai")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "go.mod"), []byte("module github.com/example/root/x/provider/openai\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Parent submodule
	parentDir := filepath.Join(root, "x", "tool")
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentDir, "go.mod"), []byte("module github.com/example/root/x/tool\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Nested submodule
	nestedDir := filepath.Join(root, "x", "tool", "bash")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "go.mod"), []byte("module github.com/example/root/x/tool/bash\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Hidden directory should be skipped
	hiddenDir := filepath.Join(root, ".pi", "agent")
	if err := os.MkdirAll(hiddenDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hiddenDir, "go.mod"), []byte("module hidden\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Vendor should be skipped
	vendorDir := filepath.Join(root, "vendor", "github.com", "example", "lib")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "go.mod"), []byte("module vendor\n"), 0644); err != nil {
		t.Fatal(err)
	}

	modules, err := discoverModules(root)
	if err != nil {
		t.Fatalf("discoverModules: %v", err)
	}

	if len(modules) != 4 {
		t.Fatalf("expected 4 modules, got %d: %+v", len(modules), modules)
	}

	want := map[string]string{
		".":                         "github.com/example/root",
		"x/provider/openai":          "github.com/example/root/x/provider/openai",
		"x/tool":                     "github.com/example/root/x/tool",
		"x/tool/bash":                "github.com/example/root/x/tool/bash",
	}

	got := make(map[string]string)
	for _, m := range modules {
		got[m.Dir] = m.Path
	}

	for dir, path := range want {
		if got[dir] != path {
			t.Errorf("module dir %q: got %q, want %q", dir, got[dir], path)
		}
	}
}
