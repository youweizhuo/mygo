package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunSimMatchesExpectedTrace(t *testing.T) {
	requirePosix(t)
	requireVerilator(t)
	tmp := t.TempDir()
	repo := repoRoot(t)

	opt := writeExportVerilogScript(t, tmp, "circt-opt.sh", testVerilogModule())
	fifoSrc := writeFile(t, tmp, "fifos.sv", fifoLibrary())
	trace := writeFile(t, tmp, "expected.sim", "verilator trace=42\n")

	args := []string{
		"--circt-opt", opt,
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

	opt := writeExportVerilogScript(t, tmp, "circt-opt.sh", testVerilogModule())
	fifoSrc := writeFile(t, tmp, "fifos.sv", fifoLibrary())
	badTrace := writeFile(t, tmp, "bad.sim", "unexpected output\n")

	args := []string{
		"--circt-opt", opt,
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

	opt := writeExportVerilogScript(t, tmp, "circt-opt.sh", testVerilogModule())
	fifoSrc := writeFile(t, tmp, "fifos.sv", fifoLibrary())
	trace := writeFile(t, tmp, "expected.sim", "verilator trace=42\n")
	verilogOut := filepath.Join(tmp, "artifacts", "design.sv")

	before := simTempDirs(t)
	args := []string{
		"--circt-opt", opt,
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

func writeExportVerilogScript(t *testing.T, dir, name, verilogBody string) string {
	script := fmt.Sprintf(`#!/bin/sh
set -e
OUT=""
IN=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --pass-pipeline=*)
      shift
      ;;
    --export-verilog)
      shift
      ;;
    -o)
      OUT="$2"
      shift 2
      ;;
    *)
      IN="$1"
      shift
      ;;
  esac
done
if [ -z "$OUT" ]; then
  echo "missing -o" >&2
  exit 1
fi
if [ -z "$IN" ]; then
  IN="/dev/stdin"
fi
cat "$IN" > "$OUT"
cat <<'__VERILOG__'
%s
__VERILOG__
`, verilogBody)
	return writeScript(t, dir, name, script)
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

func testVerilogModule() string {
	return `module main(
  input clk,
        rst
);
  reg fired = 0;
  always @(posedge clk) begin
    if (rst) begin
      fired <= 0;
    end else if (!fired) begin
      fired <= 1;
      $fwrite(32'h80000001, "verilator trace=42\n");
    end
  end
endmodule
module mygo_fifo_i32_d1();
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

func requireVerilator(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("verilator"); err != nil {
		t.Skip("verilator binary not found on PATH")
	}
}
