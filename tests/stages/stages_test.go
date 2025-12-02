package stages

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

const (
	compareLoweringOptions = "locationInfoStyle=none,omitVersionComment"
	workloadsRoot          = "tests/stages"
)

type harness struct {
	repoRoot string
	fifoLib  string
}

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
	{Name: "phi_loop", NeedsFIFO: true},
	{Name: "pipeline1", NeedsFIFO: true},
	{Name: "pipeline2", NeedsFIFO: true},
	{Name: "router_csp", NeedsFIFO: true},
}

var (
	circtOptAvailable  = checkBinary("circt-opt")
	verilatorAvailable = checkBinary("verilator")
)

func TestMLIRGeneration(t *testing.T) {
	runStageTests(t, func(t *testing.T, h harness, tc testCase) {
		dir := filepath.Join(workloadsRoot, tc.Name)
		source := filepath.Join(dir, "main.go")
		mlirGolden := filepath.Join(dir, "main.mlir.golden")
		maybeVerifyMLIR(t, h.repoRoot, source, mlirGolden)
	})
}

func TestVerilogGeneration(t *testing.T) {
	runStageTests(t, func(t *testing.T, h harness, tc testCase) {
		dir := filepath.Join(workloadsRoot, tc.Name)
		source := filepath.Join(dir, "main.go")
		verilogGolden := filepath.Join(dir, "main.sv.golden")
		maybeVerifyVerilog(t, h.repoRoot, source, verilogGolden, h.fifoLib, tc.NeedsFIFO)
	})
}

func TestSimulation(t *testing.T) {
	runStageTests(t, func(t *testing.T, h harness, tc testCase) {
		dir := filepath.Join(workloadsRoot, tc.Name)
		source := filepath.Join(dir, "main.go")
		simGolden := filepath.Join(dir, "main.sim.golden")
		maybeVerifySimulation(t, h.repoRoot, source, simGolden, h.fifoLib, tc)
	})
}

func TestSimulationDetectsMismatch(t *testing.T) {
	if !circtOptAvailable {
		t.Skip("circt-opt not on PATH")
	}
	if !verilatorAvailable {
		t.Skip("verilator not on PATH")
	}
	h := newHarness(t)
	tc := getTestCase(t, "simple")
	if tc.SimCycles <= 0 {
		t.Skip("simple workload disables simulation")
	}
	dir := filepath.Join(workloadsRoot, tc.Name)
	source := filepath.Join(dir, "main.go")
	badExpect := filepath.Join(t.TempDir(), "bad.sim")
	if err := os.WriteFile(badExpect, []byte("mismatch\n"), 0o644); err != nil {
		t.Fatalf("write bad expect: %v", err)
	}
	args := []string{
		"run", "./cmd/mygo", "sim",
		"--sim-max-cycles", strconv.Itoa(tc.SimCycles),
		"--expect", badExpect,
		source,
	}
	output := runGoCommandExpectFailure(t, h.repoRoot, args...)
	if !strings.Contains(output, "simulator output mismatch") {
		t.Fatalf("unexpected sim mismatch output: %s", output)
	}
}

func TestSimulationVerilogOutWritesArtifacts(t *testing.T) {
	if !circtOptAvailable {
		t.Skip("circt-opt not on PATH")
	}
	if !verilatorAvailable {
		t.Skip("verilator not on PATH")
	}
	h := newHarness(t)
	tc := getTestCase(t, "simple")
	if tc.SimCycles <= 0 {
		t.Skip("simple workload disables simulation")
	}
	dir := filepath.Join(workloadsRoot, tc.Name)
	source := filepath.Join(dir, "main.go")
	simGolden := filepath.Join(dir, "main.sim.golden")
	if !fileExists(t, filepath.Join(h.repoRoot, simGolden)) {
		t.Skip("simple workload missing sim golden")
	}
	verilogOut := filepath.Join(t.TempDir(), "artifacts", "design.sv")
	args := []string{
		"run", "./cmd/mygo", "sim",
		"--sim-max-cycles", strconv.Itoa(tc.SimCycles),
		"--expect", simGolden,
		"--verilog-out", verilogOut,
		source,
	}
	runGoCommand(t, h.repoRoot, args...)
	info, err := os.Stat(verilogOut)
	if err != nil {
		t.Fatalf("verilog output missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("verilog output %s empty", verilogOut)
	}
}

func runStageTests(t *testing.T, fn func(*testing.T, harness, testCase)) {
	t.Helper()
	h := newHarness(t)
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			fn(t, h, tc)
		})
	}
}

func newHarness(t *testing.T) harness {
	t.Helper()
	repoRoot := determineRepoRoot(t)
	fifoLib := filepath.Join(repoRoot, "internal", "backend", "templates", "simple_fifo.sv")
	cacheDir := filepath.Join(repoRoot, ".gocache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("create go cache dir: %v", err)
	}
	t.Setenv("GOCACHE", cacheDir)
	return harness{repoRoot: repoRoot, fifoLib: fifoLib}
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
	if !circtOptAvailable {
		t.Logf("skipping verilog check for %s: circt-opt not on PATH", source)
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
	if !circtOptAvailable {
		t.Logf("skipping simulation for %s: circt-opt not on PATH", tc.Name)
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

func runGoCommandExpectFailure(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("go %s succeeded unexpectedly", strings.Join(args, " "))
	}
	return string(out)
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

func getTestCase(t *testing.T, name string) testCase {
	t.Helper()
	for _, tc := range testCases {
		if tc.Name == name {
			return tc
		}
	}
	t.Fatalf("unknown test case %s", name)
	return testCase{}
}

func checkBinary(name string) bool {
	_, err := exec.LookPath(name)
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
