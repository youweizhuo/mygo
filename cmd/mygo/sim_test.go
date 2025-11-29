package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunSimMatchesExpectedTrace(t *testing.T) {
	requirePosix(t)
	tmp := t.TempDir()
	repo := repoRoot(t)

	translate := writeScript(t, tmp, "translate.sh", `#!/bin/sh
set -e
cat <<'EOS'
module main();
endmodule
module mygo_fifo_i32_d1();
endmodule
module mygo_fifo_i32_d4();
endmodule
module mygo_fifo_i1_d1();
endmodule
EOS
`)
	fifoSrc := writeFile(t, tmp, "fifos.sv", fifoLibrary())
	simulator := filepath.Join(repo, "scripts", "mock-sim.sh")
	trace := filepath.Join(repo, "tests", "e2e", "pipeline1", "expected.sim")
	t.Setenv("MYGO_SIM_TRACE", trace)

	args := []string{
		"--circt-translate", translate,
		"--fifo-src", fifoSrc,
		"--simulator", simulator,
		filepath.Join(repo, "tests", "e2e", "pipeline1", "main.go"),
	}
	if err := runSim(args); err != nil {
		t.Fatalf("runSim failed: %v", err)
	}
}

func TestRunSimDetectsMismatch(t *testing.T) {
	requirePosix(t)
	tmp := t.TempDir()
	repo := repoRoot(t)

	translate := writeScript(t, tmp, "translate.sh", `#!/bin/sh
set -e
cat <<'EOS'
module main();
endmodule
module mygo_fifo_i32_d1();
endmodule
module mygo_fifo_i32_d4();
endmodule
module mygo_fifo_i1_d1();
endmodule
EOS
`)
	fifoSrc := writeFile(t, tmp, "fifos.sv", fifoLibrary())
	simulator := filepath.Join(repo, "scripts", "mock-sim.sh")
	badTrace := writeFile(t, tmp, "bad.sim", "unexpected output\n")
	t.Setenv("MYGO_SIM_TRACE", badTrace)

	args := []string{
		"--circt-translate", translate,
		"--fifo-src", fifoSrc,
		"--simulator", simulator,
		filepath.Join(repo, "tests", "e2e", "pipeline1", "main.go"),
	}
	err := runSim(args)
	if err == nil || !strings.Contains(err.Error(), "simulator output mismatch") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestRunSimWithVerilogOutDoesNotLeakTempDir(t *testing.T) {
	requirePosix(t)
	tmp := t.TempDir()
	repo := repoRoot(t)

	translate := writeScript(t, tmp, "translate.sh", `#!/bin/sh
set -e
cat <<'EOS'
module main();
endmodule
module mygo_fifo_i32_d1();
endmodule
module mygo_fifo_i32_d4();
endmodule
module mygo_fifo_i1_d1();
endmodule
EOS
`)
	fifoSrc := writeFile(t, tmp, "fifos.sv", fifoLibrary())
	simulator := filepath.Join(repo, "scripts", "mock-sim.sh")
	trace := filepath.Join(repo, "tests", "e2e", "pipeline1", "expected.sim")
	t.Setenv("MYGO_SIM_TRACE", trace)
	verilogOut := filepath.Join(tmp, "artifacts", "design.sv")

	before := simTempDirs(t)
	args := []string{
		"--circt-translate", translate,
		"--fifo-src", fifoSrc,
		"--simulator", simulator,
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

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("determine repo root: %v", err)
	}
	return root
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
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

func fifoLibrary() string {
	return `module mygo_fifo_i32_d1();
endmodule
module mygo_fifo_i32_d4();
endmodule
module mygo_fifo_i1_d1();
endmodule
`
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
