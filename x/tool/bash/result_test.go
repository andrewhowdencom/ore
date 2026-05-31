package bash

import (
	"strings"
	"testing"
)

func TestResult_MarshalMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		result   Result
		want     []string
		notWant  []string
	}{
		{
			name: "both streams present, success",
			result: Result{
				Stdout:   "hello world\nline two",
				Stderr:   "warning: something\n",
				ExitCode: 0,
			},
			want: []string{
				"**stdout**",
				"```",
				"hello world",
				"line two",
				"```",
				"**stderr**",
				"warning: something",
				"**exit code:** 0",
			},
		},
		{
			name: "non-zero exit code",
			result: Result{
				Stdout:   "",
				Stderr:   "fatal error",
				ExitCode: 1,
			},
			want: []string{
				"**stderr**",
				"fatal error",
				"**exit code:** 1",
			},
			notWant: []string{
				"**stdout**",
			},
		},
		{
			name: "empty stderr omitted",
			result: Result{
				Stdout:   "only stdout",
				Stderr:   "",
				ExitCode: 0,
			},
			want: []string{
				"**stdout**",
				"only stdout",
				"**exit code:** 0",
			},
			notWant: []string{
				"**stderr**",
			},
		},
		{
			name: "empty stdout omitted",
			result: Result{
				Stdout:   "",
				Stderr:   "only stderr",
				ExitCode: 0,
			},
			want: []string{
				"**stderr**",
				"only stderr",
				"**exit code:** 0",
			},
			notWant: []string{
				"**stdout**",
			},
		},
		{
			name: "both empty",
			result: Result{
				Stdout:   "",
				Stderr:   "",
				ExitCode: 0,
			},
			want:    []string{"**exit code:** 0"},
			notWant: []string{"**stdout**", "**stderr**"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := tt.result.MarshalMarkdown()
			for _, s := range tt.want {
				if !strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() missing expected substring %q\ngot:\n%s", s, md)
				}
			}
			for _, s := range tt.notWant {
				if strings.Contains(md, s) {
					t.Errorf("MarshalMarkdown() unexpectedly contains %q\ngot:\n%s", s, md)
				}
			}
		})
	}
}
