package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mygo/internal/ir"
)

func TestDefaultSimExpectPathFileInput(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "program", "main.go")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatalf("mkdir for file: %v", err)
	}
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got := defaultSimExpectPath(file)
	want := filepath.Join(filepath.Dir(file), "expected.sim")
	if got != want {
		t.Fatalf("defaultSimExpectPath(%s)=%s, want %s", file, got, want)
	}
}

func TestDefaultSimExpectPathDirectoryInput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "program")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	got := defaultSimExpectPath(dir)
	want := filepath.Join(dir, "expected.sim")
	if got != want {
		t.Fatalf("defaultSimExpectPath(%s)=%s, want %s", dir, got, want)
	}
}

func TestParseSimArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "single", in: "foo", want: []string{"foo"}},
		{name: "dedupe spaces", in: "  foo   bar\tbaz  ", want: []string{"foo", "bar", "baz"}},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmpSlice(tc.want, parseSimArgs(tc.in)); diff != "" {
				t.Fatalf("parseSimArgs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSimulationTempRoot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		inputs []string
		setup  func(base string) []string
		expect func(base string) string
	}{
		{
			name: "file input chooses parent dir",
			setup: func(base string) []string {
				dir := filepath.Join(base, "proj")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				file := filepath.Join(dir, "main.go")
				if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
					t.Fatalf("write file: %v", err)
				}
				return []string{file}
			},
			expect: func(base string) string {
				return filepath.Join(base, "proj")
			},
		},
		{
			name: "dir input returned as-is",
			setup: func(base string) []string {
				dir := filepath.Join(base, "pkg")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				return []string{dir}
			},
			expect: func(base string) string {
				return filepath.Join(base, "pkg")
			},
		},
		{
			name: "fallback to cwd when nothing exists",
			setup: func(base string) []string {
				return []string{filepath.Join(base, "missing", "main.go")}
			},
			expect: func(_ string) string {
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatalf("getwd: %v", err)
				}
				return cwd
			},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			inputs := tc.setup(base)
			got := simulationTempRoot(inputs)
			want := tc.expect(base)
			wantAbs, err := filepath.Abs(want)
			if err != nil {
				t.Fatalf("abs want: %v", err)
			}
			if got != wantAbs {
				t.Fatalf("simulationTempRoot mismatch: got %s want %s", got, wantAbs)
			}
		})
	}
}

func TestPrependPathToEnv(t *testing.T) {
	dir := t.TempDir()
	const oldPath = "/usr/bin"
	t.Setenv("PATH", oldPath)
	got := pathValue(prependPathToEnv(dir))
	want := dir + string(os.PathListSeparator) + oldPath
	if got != want {
		t.Fatalf("prependPathToEnv path=%s, want %s", got, want)
	}

	t.Setenv("PATH", "")
	got = pathValue(prependPathToEnv(dir))
	if got != dir {
		t.Fatalf("prependPathToEnv empty PATH=%s, want %s", got, dir)
	}
}

func TestDesignHasChannels(t *testing.T) {
	t.Parallel()
	channel := &ir.Channel{Name: "ch", Type: &ir.SignalType{Width: 32}}
	cases := []struct {
		name   string
		design *ir.Design
		want   bool
	}{
		{name: "nil design", design: nil, want: false},
		{name: "module without channels", design: &ir.Design{Modules: []*ir.Module{{Name: "foo"}}}, want: false},
		{name: "module with empty entry", design: &ir.Design{Modules: []*ir.Module{nil}}, want: false},
		{name: "module with channel", design: &ir.Design{Modules: []*ir.Module{{Name: "foo", Channels: map[string]*ir.Channel{"ch": channel}}}}, want: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := designHasChannels(tc.design); got != tc.want {
				t.Fatalf("designHasChannels(%s)=%t, want %t", tc.name, got, tc.want)
			}
		})
	}
}

func cmpSlice(want, got []string) string {
	if len(want) != len(got) {
		return fmt.Sprintf("length mismatch: want %d, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if want[i] != got[i] {
			return fmt.Sprintf("element %d mismatch: want %q, got %q", i, want[i], got[i])
		}
	}
	return ""
}

func pathValue(env []string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			return strings.TrimPrefix(kv, "PATH=")
		}
	}
	return ""
}
