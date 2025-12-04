package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"

	"mygo/internal/backend"
	"mygo/internal/diag"
	"mygo/internal/frontend"
	"mygo/internal/ir"
	"mygo/internal/mlir"
	"mygo/internal/passes"
	"mygo/internal/validate"
)

var emitVerilog = backend.EmitVerilog

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printGlobalUsage()
		return fmt.Errorf("missing command")
	}

	switch args[0] {
	case "compile":
		return runCompile(args[1:])
	case "sim":
		return runSim(args[1:])
	case "lint":
		return runLint(args[1:])
	default:
		printGlobalUsage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runCompile(args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	emit := fs.String("emit", "mlir", "output format (ssa|ir|mlir|verilog)")
	output := fs.String("o", "", "output file path (stdout when omitted, except verilog)")
	target := fs.String("target", "main", "target function or module")
	diagFormat := fs.String("diag-format", "text", "diagnostic output format (text|json)")
	circtOpt := fs.String("circt-opt", "", "path to circt-opt (optional, falls back to PATH lookup)")
	circtPipeline := fs.String("circt-pipeline", "", "circt-opt --pass-pipeline string (optional)")
	circtLowering := fs.String("circt-lowering-options", "", "comma-separated circt-opt --lowering-options string (optional)")
	circtMLIR := fs.String("circt-mlir", "", "path to dump the MLIR handed to CIRCT (optional)")
	fifoSrc := fs.String("fifo-src", "", "path to FIFO implementation source (required when channels are present)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = target

	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("compile command requires at least one Go source file")
	}

	inputs := fs.Args()
	result, err := prepareProgram(inputs, *diagFormat)
	if err != nil {
		return err
	}

	if *emit == "ssa" {
		return emitSSAProgram(result.program, *output)
	}

	if err := validateProgram(result); err != nil {
		return err
	}

	design, err := ir.BuildDesign(result.program, result.reporter)
	if err != nil {
		return err
	}

	if err := runDefaultPasses(design, result.reporter); err != nil {
		return err
	}
	hasChannels := designHasChannels(design)

	switch *emit {
	case "ir":
		return emitIRDesign(design, *output)
	case "mlir":
		return mlir.Emit(design, *output)
	case "verilog":
		if *output == "" || *output == "-" {
			return fmt.Errorf("verilog emission requires -o when auxiliary FIFO sources are generated")
		}
		if hasChannels && *fifoSrc == "" {
			return fmt.Errorf("verilog emission requires --fifo-src when design contains channels")
		}
		opts := backend.Options{
			CIRCTOptPath:    *circtOpt,
			PassPipeline:    *circtPipeline,
			LoweringOptions: *circtLowering,
			DumpMLIRPath:    *circtMLIR,
			FIFOSource:      *fifoSrc,
		}
		res, err := emitVerilog(design, *output, opts)
		if err != nil {
			return err
		}
		if len(res.AuxPaths) > 0 {
			fmt.Fprintf(os.Stderr, "additional sources written: %s\n", strings.Join(res.AuxPaths, ", "))
		}
		return nil
	default:
		return fmt.Errorf("unknown emit format: %s", *emit)
	}

}

func printGlobalUsage() {
	fmt.Fprintf(os.Stderr, "MyGO compiler (phase 1 scaffold)\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  mygo <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  compile    Compile Go source to SSA, IR, MLIR, or Verilog\n")
	fmt.Fprintf(os.Stderr, "  sim        Compile to Verilog and run a simulator\n")
	fmt.Fprintf(os.Stderr, "  lint       Run validation-only checks (e.g. concurrency rules)\n")
}

func runLint(args []string) error {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	concurrency := fs.Bool("concurrency", true, "enable concurrency validation rules")
	diagFormat := fs.String("diag-format", "text", "diagnostic output format (text|json)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("lint requires at least one Go source file")
	}

	result, err := prepareProgram(fs.Args(), *diagFormat)
	if err != nil {
		return err
	}

	if *concurrency {
		if err := validateProgram(result); err != nil {
			return err
		}
	}

	return nil
}

type frontendResult struct {
	reporter *diag.Reporter
	program  *ssa.Program
	ssaPkgs  []*ssa.Package
	pkgs     []*packages.Package
}

func prepareProgram(sources []string, diagFormat string) (*frontendResult, error) {
	reporter := diag.NewReporter(os.Stderr, diagFormat)
	cfg := frontend.LoadConfig{Sources: sources}
	pkgs, _, err := frontend.LoadPackages(cfg, reporter)
	if err != nil {
		return nil, err
	}
	if reporter.HasErrors() {
		return nil, fmt.Errorf("errors reported while loading packages")
	}
	prog, ssaPkgs, err := frontend.BuildSSA(pkgs, reporter)
	if err != nil {
		return nil, err
	}
	if reporter.HasErrors() {
		return nil, fmt.Errorf("errors reported during SSA construction")
	}
	return &frontendResult{
		reporter: reporter,
		program:  prog,
		ssaPkgs:  ssaPkgs,
		pkgs:     pkgs,
	}, nil
}

func runDefaultPasses(design *ir.Design, reporter *diag.Reporter) error {
	passMgr := passes.NewManager()
	passMgr.Add(passes.NewWidthInference(reporter))
	if err := passMgr.Run(design); err != nil {
		return err
	}
	if reporter != nil && reporter.HasErrors() {
		return fmt.Errorf("analysis passes reported errors")
	}
	return nil
}

func validateProgram(result *frontendResult) error {
	if result == nil || result.program == nil {
		return fmt.Errorf("no program available for validation")
	}
	if err := validate.CheckProgram(result.program, result.ssaPkgs, result.pkgs, result.reporter); err != nil {
		return err
	}
	return nil
}

func runSim(args []string) error {
	fs := flag.NewFlagSet("sim", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	diagFormat := fs.String("diag-format", "text", "diagnostic output format (text|json)")
	circtOpt := fs.String("circt-opt", "", "path to circt-opt (optional)")
	circtPipeline := fs.String("circt-pipeline", "", "circt-opt --pass-pipeline string (optional)")
	circtLowering := fs.String("circt-lowering-options", "", "comma-separated circt-opt --lowering-options string (optional)")
	circtMLIR := fs.String("circt-mlir", "", "path to dump the MLIR handed to CIRCT (optional)")
	verilogOut := fs.String("verilog-out", "", "path to write the emitted Verilog bundle (optional)")
	keepArtifacts := fs.Bool("keep-artifacts", true, "keep temporary artifacts generated during simulation")
	simulator := fs.String("simulator", "", "simulator executable to run (e.g. a Verilator wrapper script)")
	simArgs := fs.String("sim-args", "", "additional simulator arguments (space-separated)")
	expectPath := fs.String("expect", "", "path to file containing expected simulator stdout (optional)")
	fifoSrc := fs.String("fifo-src", "", "path to FIFO implementation source (required when channels are present)")
	simMaxCycles := fs.Int("sim-max-cycles", 16, "maximum clock cycles to run when using the default Verilator simulator")
	simResetCycles := fs.Int("sim-reset-cycles", 2, "number of initial cycles to hold reset asserted for the default simulator")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("sim requires at least one Go source file")
	}

	inputs := fs.Args()

	if *expectPath == "" && len(inputs) == 1 {
		if candidate := defaultSimExpectPath(inputs[0]); candidate != "" {
			if _, err := os.Stat(candidate); err == nil {
				*expectPath = candidate
			}
		}
	}

	result, err := prepareProgram(inputs, *diagFormat)
	if err != nil {
		return err
	}

	if err := validateProgram(result); err != nil {
		return err
	}

	design, err := ir.BuildDesign(result.program, result.reporter)
	if err != nil {
		return err
	}

	if err := runDefaultPasses(design, result.reporter); err != nil {
		return err
	}

	hasChannels := designHasChannels(design)
	tempRoot := simulationTempRoot(inputs)

	var tempDir string
	if *verilogOut == "" {
		var err error
		tempDir, err = os.MkdirTemp(tempRoot, "mygo-sim-*")
		if err != nil {
			return err
		}
		if !*keepArtifacts {
			defer os.RemoveAll(tempDir)
		}
	}

	svPath := *verilogOut
	if svPath == "" {
		svPath = filepath.Join(tempDir, "design.sv")
	} else if err := os.MkdirAll(filepath.Dir(svPath), 0o755); err != nil {
		return err
	}

	opts := backend.Options{
		CIRCTOptPath:    *circtOpt,
		PassPipeline:    *circtPipeline,
		LoweringOptions: *circtLowering,
		DumpMLIRPath:    *circtMLIR,
		KeepTemps:       *keepArtifacts,
		FIFOSource:      *fifoSrc,
	}

	if hasChannels && *fifoSrc == "" {
		return fmt.Errorf("simulation requires --fifo-src when design contains channels")
	}

	res, err := emitVerilog(design, svPath, opts)
	if err != nil {
		return err
	}
	svPath = res.MainPath
	auxFiles := append([]string{}, res.AuxPaths...)

	if *simulator == "" {
		return runBuiltinVerilator(svPath, auxFiles, *expectPath, *simMaxCycles, *simResetCycles, tempRoot, *keepArtifacts)
	}

	simulatorArgs := parseSimArgs(*simArgs)
	simulatorArgs = append(simulatorArgs, svPath)
	simulatorArgs = append(simulatorArgs, auxFiles...)
	cmd := exec.Command(*simulator, simulatorArgs...)

	var stdoutBuf bytes.Buffer
	if *expectPath != "" {
		cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("simulator failed: %w", err)
	}

	if *expectPath != "" {
		if err := compareSimulatorOutput(*expectPath, stdoutBuf.Bytes()); err != nil {
			return err
		}
	}

	return nil
}

func parseSimArgs(raw string) []string {
	if raw == "" {
		return nil
	}
	fields := strings.Fields(raw)
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			result = append(result, f)
		}
	}
	return result
}

func designHasChannels(design *ir.Design) bool {
	if design == nil {
		return false
	}
	for _, module := range design.Modules {
		if module == nil {
			continue
		}
		if len(module.Channels) > 0 {
			return true
		}
	}
	return false
}

func defaultSimExpectPath(input string) string {
	if input == "" {
		return ""
	}
	cleaned := filepath.Clean(input)
	if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
		return filepath.Join(cleaned, "expected.sim")
	}
	dir := filepath.Dir(cleaned)
	return filepath.Join(dir, "expected.sim")
}

func simulationTempRoot(inputs []string) string {
	for _, in := range inputs {
		if dir := resolveInputDir(in); dir != "" {
			return dir
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

func resolveInputDir(input string) string {
	if input == "" {
		return ""
	}
	cleaned := filepath.Clean(input)
	if dir := existingDirectory(cleaned); dir != "" {
		return dir
	}
	parent := filepath.Dir(cleaned)
	return existingDirectory(parent)
}

func existingDirectory(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func runBuiltinVerilator(mainPath string, auxPaths []string, expectPath string, maxCycles, resetCycles int, tempRoot string, keepArtifacts bool) error {
	if maxCycles <= 0 {
		return fmt.Errorf("default simulator requires --sim-max-cycles > 0 (got %d)", maxCycles)
	}
	if resetCycles < 0 {
		return fmt.Errorf("default simulator requires --sim-reset-cycles >= 0 (got %d)", resetCycles)
	}
	verilatorPath, err := exec.LookPath("verilator")
	if err != nil {
		return fmt.Errorf("resolve verilator: %w", err)
	}

	tempDir, err := os.MkdirTemp(tempRoot, "mygo-verilator-*")
	if err != nil {
		return fmt.Errorf("create verilator temp dir: %w", err)
	}
	if !keepArtifacts {
		defer os.RemoveAll(tempDir)
	}

	driverPath := filepath.Join(tempDir, "sim_main.cpp")
	driver, err := renderVerilatorDriver(maxCycles, resetCycles)
	if err != nil {
		return fmt.Errorf("render verilator driver: %w", err)
	}
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		return fmt.Errorf("write verilator driver: %w", err)
	}
	if _, err := installXargsShim(tempDir); err != nil {
		return err
	}

	objDir := filepath.Join(tempDir, "obj_dir")
	args := []string{
		"--cc", "--exe", "--build",
		"--sv",
		"--Mdir", objDir,
		"--top-module", "main",
		"-o", "mygo_sim",
	}
	args = append(args, mainPath)
	args = append(args, auxPaths...)
	args = append(args, driverPath)

	cmd := exec.Command(verilatorPath, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = prependPathToEnv(tempDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("verilator build failed: %w", err)
	}

	simPath := filepath.Join(objDir, "mygo_sim")
	simCmd := exec.Command(simPath)
	var stdoutBuf bytes.Buffer
	if expectPath != "" {
		simCmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	} else {
		simCmd.Stdout = os.Stdout
	}
	simCmd.Stderr = os.Stderr
	if err := simCmd.Run(); err != nil {
		return fmt.Errorf("verilator simulation failed: %w", err)
	}
	if expectPath != "" {
		if err := compareSimulatorOutput(expectPath, stdoutBuf.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

func compareSimulatorOutput(expectPath string, got []byte) error {
	want, err := os.ReadFile(expectPath)
	if err != nil {
		return fmt.Errorf("read expect file: %w", err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		return fmt.Errorf("simulator output mismatch\nexpected:\n%s\nactual:\n%s", string(want), string(got))
	}
	return nil
}

func prependPathToEnv(dir string) []string {
	env := os.Environ()
	newPath := dir
	currentPath := os.Getenv("PATH")
	if currentPath != "" {
		newPath = dir + string(os.PathListSeparator) + currentPath
	}
	replaced := false
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + newPath
			replaced = true
			break
		}
	}
	if !replaced {
		env = append(env, "PATH="+newPath)
	}
	return env
}

func emitSSAProgram(prog *ssa.Program, outputPath string) error {
	return withOutputWriter(outputPath, func(w io.Writer) error {
		pkgs := sortedSSAPackages(prog)
		if len(pkgs) == 0 {
			return fmt.Errorf("no SSA packages available to emit")
		}
		for i, pkg := range pkgs {
			if i > 0 {
				fmt.Fprintln(w)
			}
			if _, err := pkg.WriteTo(w); err != nil {
				return err
			}
		}
		return nil
	})
}

func emitIRDesign(design *ir.Design, outputPath string) error {
	if design == nil {
		return fmt.Errorf("no IR design available to emit")
	}
	return withOutputWriter(outputPath, func(w io.Writer) error {
		ir.Dump(design, w)
		return nil
	})
}

func sortedSSAPackages(prog *ssa.Program) []*ssa.Package {
	if prog == nil {
		return nil
	}
	all := prog.AllPackages()
	pkgs := make([]*ssa.Package, 0, len(all))
	for _, pkg := range all {
		if pkg == nil {
			continue
		}
		pkgs = append(pkgs, pkg)
	}
	sort.Slice(pkgs, func(i, j int) bool {
		return packageSortKey(pkgs[i]) < packageSortKey(pkgs[j])
	})
	return pkgs
}

func packageSortKey(pkg *ssa.Package) string {
	if pkg == nil {
		return ""
	}
	if pkg.Pkg != nil {
		return pkg.Pkg.Path()
	}
	return pkg.String()
}

func withOutputWriter(path string, fn func(io.Writer) error) error {
	w, cleanup, err := outputWriter(path)
	if err != nil {
		return err
	}
	if cleanup == nil {
		return fn(w)
	}
	err = fn(w)
	if closeErr := cleanup(); err == nil && closeErr != nil {
		err = closeErr
	}
	return err
}

func outputWriter(path string) (io.Writer, func() error, error) {
	if path == "" || path == "-" {
		return os.Stdout, nil, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}
