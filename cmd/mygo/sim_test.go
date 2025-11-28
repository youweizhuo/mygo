package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSimCommandWithStubSimulator(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sim command test requires POSIX shell")
	}

	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	tmp := t.TempDir()

	translate := writeScript(t, tmp, "translate.sh", `#!/bin/sh
set -e
INPUT=""
for arg in "$@"; do
  case "$arg" in
    --export-verilog) shift ;;
    *) INPUT="$arg"; shift ;;
  esac
done
cat "$INPUT"
`)

	expectPath := filepath.Join(tmp, "expect.txt")
	if err := os.WriteFile(expectPath, []byte("simulation-ok\n"), 0o644); err != nil {
		t.Fatalf("write expect: %v", err)
	}

	simulator := writeScript(t, tmp, "sim.sh", `#!/bin/sh
set -e
echo "simulation-ok"
`)

	cmd := exec.Command("go", "run", "./cmd/mygo", "sim",
		"--circt-translate", translate,
		"--simulator", simulator,
		"--expect", expectPath,
		"test/e2e/simple/main.go",
	)
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mygo sim failed: %v\n%s", err, string(out))
	}
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}
