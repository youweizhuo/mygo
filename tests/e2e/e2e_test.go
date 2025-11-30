package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const compareLoweringOptions = "locationInfoStyle=none,omitVersionComment"

type testCase struct {
	Name      string
	NeedsFIFO bool
	SimCycles int
}

var testCases = []testCase{
	{Name: "simple", SimCycles: 1},
	{Name: "simple_branch"},
	{Name: "simple_print"},
	{Name: "type_mismatch", SimCycles: 1},
	{Name: "comb_adder", SimCycles: 1},
	{Name: "comb_bitwise", SimCycles: 1},
	{Name: "comb_concat", SimCycles: 1},
	{Name: "simple_channel", NeedsFIFO: true},
	{Name: "phi_loop"},
	{Name: "pipeline1", NeedsFIFO: true},
	{Name: "pipeline2", NeedsFIFO: true},
	{Name: "router_csp", NeedsFIFO: true},
}

var verilatorAvailable = checkVerilator()

func TestProgramsLoweringAndSimulation(t *testing.T) {
	repoRoot := determineRepoRoot(t)
	fifoLib := filepath.Join(repoRoot, "internal", "backend", "templates", "simple_fifo.sv")
	cacheDir := filepath.Join(repoRoot, ".gocache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("create go cache dir: %v", err)
	}
	t.Setenv("GOCACHE", cacheDir)

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join("tests", "e2e", tc.Name)
			source := filepath.Join(dir, "main.go")
			maybeVerifyMLIR(t, repoRoot, source, filepath.Join(dir, "main.mlir.golden"))
			maybeVerifyVerilog(t, repoRoot, source, filepath.Join(dir, "main.sv.golden"), fifoLib, tc.NeedsFIFO)
			maybeVerifySimulation(t, repoRoot, source, filepath.Join(dir, "main.sim.golden"), fifoLib, tc)
		})
	}
}

func maybeVerifyMLIR(t *testing.T, repoRoot, source, golden string) {
	t.Helper()
	if !fileExists(t, filepath.Join(repoRoot, golden)) {
		return
	}
	output := filepath.Join(t.TempDir(), "main.mlir")
	args := []string{"run", "./cmd/mygo", "compile", "-emit=mlir", "-o", output, source}
	runGoCommand(t, repoRoot, args...)
	compareTextFiles(t, filepath.Join(repoRoot, golden), output)
}

func maybeVerifyVerilog(t *testing.T, repoRoot, source, golden, fifoLib string, needsFIFO bool) {
	t.Helper()
	if !fileExists(t, filepath.Join(repoRoot, golden)) {
		return
	}
	output := filepath.Join(t.TempDir(), "main.sv")
	args := []string{
		"run", "./cmd/mygo", "compile",
		"-emit=verilog",
		"--circt-lowering-options", compareLoweringOptions,
		"-o", output,
	}
	if needsFIFO {
		args = append(args, "--fifo-src", fifoLib)
	}
	args = append(args, source)
	runGoCommand(t, repoRoot, args...)
	compareTextFiles(t, filepath.Join(repoRoot, golden), output)
}

func maybeVerifySimulation(t *testing.T, repoRoot, source, golden, fifoLib string, tc testCase) {
	t.Helper()
	if !fileExists(t, filepath.Join(repoRoot, golden)) || tc.SimCycles <= 0 {
		if tc.SimCycles <= 0 {
			t.Logf("skipping simulation for %s: sim cycles disabled", tc.Name)
		}
		return
	}
	if !verilatorAvailable {
		t.Logf("skipping simulation for %s: verilator not on PATH", tc.Name)
		return
	}
	args := []string{
		"run", "./cmd/mygo", "sim",
		"--sim-max-cycles", strconv.Itoa(tc.SimCycles),
		"--expect", golden,
	}
	if tc.NeedsFIFO {
		args = append(args, "--fifo-src", fifoLib)
	}
	args = append(args, source)
	runGoCommand(t, repoRoot, args...)
}

func runGoCommand(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false
		}
		t.Fatalf("stat %s: %v", path, err)
	}
	return true
}

func compareTextFiles(t *testing.T, golden, actual string) {
	t.Helper()
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %s: %v", golden, err)
	}
	got, err := os.ReadFile(actual)
	if err != nil {
		t.Fatalf("read actual %s: %v", actual, err)
	}
	if bytes.Equal(want, got) {
		return
	}
	if diff := cmp.Diff(string(want), string(got)); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

func checkVerilator() bool {
	_, err := exec.LookPath("verilator")
	return err == nil
}

func determineRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("determine repo root: %v", err)
	}
	return root
}
