package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"text/template"

	"mygo/internal/backend"
	"mygo/internal/ir"
)

func TestRunSimMatchesExpectedTrace(t *testing.T) {
	requirePosix(t)
	requireVerilator(t)
	tmp := t.TempDir()
	repo := repoRoot(t)

	stubEmitVerilog(t, "verilog_main.sv")
	fifoSrc := writeFifoLibrary(t, tmp, []fifoSpec{
		{Width: 32, Depth: 1},
		{Width: 32, Depth: 4},
		{Width: 1, Depth: 1},
	})
	trace := writeFile(t, tmp, "expected.sim", "verilator trace=42\n")

	args := []string{
		"--fifo-src", fifoSrc,
		"--sim-max-cycles", "4",
		"--expect", trace,
		filepath.Join(repo, "tests", "e2e", "pipeline1", "main.go"),
	}
	if err := runSim(args); err != nil {
		t.Fatalf("runSim failed: %v", err)
	}
}

func TestRunSimDetectsMismatch(t *testing.T) {
	requirePosix(t)
	requireVerilator(t)
	tmp := t.TempDir()
	repo := repoRoot(t)

	stubEmitVerilog(t, "verilog_main.sv")
	fifoSrc := writeFifoLibrary(t, tmp, []fifoSpec{
		{Width: 32, Depth: 1},
		{Width: 32, Depth: 4},
		{Width: 1, Depth: 1},
	})
	badTrace := writeFile(t, tmp, "bad.sim", "unexpected output\n")

	args := []string{
		"--fifo-src", fifoSrc,
		"--sim-max-cycles", "4",
		"--expect", badTrace,
		filepath.Join(repo, "tests", "e2e", "pipeline1", "main.go"),
	}
	err := runSim(args)
	if err == nil || !strings.Contains(err.Error(), "simulator output mismatch") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestRunSimWithVerilogOutDoesNotLeakTempDir(t *testing.T) {
	requirePosix(t)
	requireVerilator(t)
	tmp := t.TempDir()
	repo := repoRoot(t)

	stubEmitVerilog(t, "verilog_main.sv")
	fifoSrc := writeFifoLibrary(t, tmp, []fifoSpec{
		{Width: 32, Depth: 1},
		{Width: 32, Depth: 4},
		{Width: 1, Depth: 1},
	})
	trace := writeFile(t, tmp, "expected.sim", "verilator trace=42\n")
	verilogOut := filepath.Join(tmp, "artifacts", "design.sv")

	before := simTempDirs(t)
	args := []string{
		"--fifo-src", fifoSrc,
		"--sim-max-cycles", "4",
		"--expect", trace,
		"--verilog-out", verilogOut,
		filepath.Join(repo, "tests", "e2e", "pipeline1", "main.go"),
	}
	if err := runSim(args); err != nil {
		t.Fatalf("runSim failed: %v", err)
	}
	if _, err := os.Stat(verilogOut); err != nil {
		t.Fatalf("expected verilog output at %s: %v", verilogOut, err)
	}
	after := simTempDirs(t)
	for dir := range after {
		if _, ok := before[dir]; !ok {
			t.Fatalf("runSim leaked temp dir %s", dir)
		}
	}
}

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

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("determine repo root: %v", err)
	}
	return root
}

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "cmd", "mygo", "testdata", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("testdata %s: %v", name, err)
	}
	return path
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	path := testdataPath(t, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read testdata %s: %v", name, err)
	}
	return string(data)
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
	return path
}

type fifoSpec struct {
	Width int
	Depth int
}

func writeFifoLibrary(t *testing.T, dir string, fifos []fifoSpec) string {
	t.Helper()
	body := executeTestTemplate(t, "fifo_library.sv.tmpl", fifos)
	return writeFile(t, dir, "fifos.sv", body)
}

func executeTestTemplate(t *testing.T, name string, data any) string {
	t.Helper()
	path := testdataPath(t, name)
	tmpl, err := template.ParseFiles(path)
	if err != nil {
		t.Fatalf("parse template %s: %v", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute template %s: %v", name, err)
	}
	return buf.String()
}

func stubEmitVerilog(t *testing.T, verilogFixture string) {
	t.Helper()
	verilog := readTestdata(t, verilogFixture)
	prev := emitVerilog
	emitVerilog = func(design *ir.Design, outputPath string, opts backend.Options) (backend.Result, error) {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return backend.Result{}, err
		}
		if err := os.WriteFile(outputPath, []byte(verilog), 0o644); err != nil {
			return backend.Result{}, err
		}
		return backend.Result{MainPath: outputPath}, nil
	}
	t.Cleanup(func() { emitVerilog = prev })
}

func simTempDirs(t *testing.T) map[string]struct{} {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "mygo-sim-*"))
	if err != nil {
		t.Fatalf("glob sim temp dirs: %v", err)
	}
	set := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		set[m] = struct{}{}
	}
	return set
}

func requirePosix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tests require a POSIX shell")
	}
}

func requireVerilator(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("verilator"); err != nil {
		t.Skip("verilator binary not found on PATH")
	}
}
