package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	case "dump-ssa":
		return runDumpSSA(args[1:])
	case "dump-ir":
		return runDumpIR(args[1:])
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

	emit := fs.String("emit", "mlir", "output format (mlir|verilog)")
	output := fs.String("o", "", "output file path")
	target := fs.String("target", "main", "target function or module")
	diagFormat := fs.String("diag-format", "text", "diagnostic output format (text|json)")
	circtOpt := fs.String("circt-opt", "", "path to circt-opt (optional, falls back to PATH lookup)")
	circtTranslate := fs.String("circt-translate", "", "path to circt-translate (optional, falls back to PATH lookup)")
	circtPipeline := fs.String("circt-pipeline", "", "circt-opt --pass-pipeline string (optional)")
	circtMLIR := fs.String("circt-mlir", "", "path to dump the MLIR handed to CIRCT (optional)")

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

	switch *emit {
	case "mlir":
		return mlir.Emit(design, *output)
	case "verilog":
		opts := backend.Options{
			CIRCTOptPath:       *circtOpt,
			CIRCTTranslatePath: *circtTranslate,
			PassPipeline:       *circtPipeline,
			DumpMLIRPath:       *circtMLIR,
		}
		return backend.EmitVerilog(design, *output, opts)
	default:
		return fmt.Errorf("unknown emit format: %s", *emit)
	}

}

func printGlobalUsage() {
	fmt.Fprintf(os.Stderr, "MyGO compiler (phase 1 scaffold)\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  mygo <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  compile    Compile Go source to MLIR or Verilog\n")
	fmt.Fprintf(os.Stderr, "  sim        Compile to Verilog and run a simulator\n")
	fmt.Fprintf(os.Stderr, "  dump-ssa   Load Go sources and dump SSA form\n")
	fmt.Fprintf(os.Stderr, "  dump-ir    Translate SSA into the MyGO hardware IR and dump it\n")
	fmt.Fprintf(os.Stderr, "  lint       Run validation-only checks (e.g. concurrency rules)\n")
}

func runDumpSSA(args []string) error {
	fs := flag.NewFlagSet("dump-ssa", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	diagFormat := fs.String("diag-format", "text", "diagnostic output format (text|json)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("dump-ssa requires at least one Go source file")
	}

	result, err := prepareProgram(fs.Args(), *diagFormat)
	if err != nil {
		return err
	}

	for _, pkg := range result.ssaPkgs {
		if pkg == nil {
			continue
		}
		pkg.WriteTo(os.Stdout)
	}

	return nil
}

func runDumpIR(args []string) error {
	fs := flag.NewFlagSet("dump-ir", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	diagFormat := fs.String("diag-format", "text", "diagnostic output format (text|json)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("dump-ir requires at least one Go source file")
	}

	result, err := prepareProgram(fs.Args(), *diagFormat)
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

	ir.Dump(design, os.Stdout)
	return nil
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
	circtTranslate := fs.String("circt-translate", "", "path to circt-translate (optional)")
	circtPipeline := fs.String("circt-pipeline", "", "circt-opt --pass-pipeline string (optional)")
	circtMLIR := fs.String("circt-mlir", "", "path to dump the MLIR handed to CIRCT (optional)")
	verilogOut := fs.String("verilog-out", "", "path to write the emitted Verilog bundle (optional)")
	keepArtifacts := fs.Bool("keep-artifacts", false, "keep temporary artifacts generated during simulation")
	simulator := fs.String("simulator", "", "simulator executable to run (e.g. verilator, iverilog, or a wrapper script)")
	simArgs := fs.String("sim-args", "", "additional simulator arguments (space-separated)")
	expectPath := fs.String("expect", "", "path to file containing expected simulator stdout (optional)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("sim requires at least one Go source file")
	}

	inputs := fs.Args()
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

	tempDir, err := os.MkdirTemp("", "mygo-sim-*")
	if err != nil {
		return err
	}
	if !*keepArtifacts && *verilogOut == "" {
		defer os.RemoveAll(tempDir)
	}

	svPath := *verilogOut
	if svPath == "" {
		svPath = filepath.Join(tempDir, "design.sv")
	} else if err := os.MkdirAll(filepath.Dir(svPath), 0o755); err != nil {
		return err
	}

	opts := backend.Options{
		CIRCTOptPath:       *circtOpt,
		CIRCTTranslatePath: *circtTranslate,
		PassPipeline:       *circtPipeline,
		DumpMLIRPath:       *circtMLIR,
		KeepTemps:          *keepArtifacts,
	}

	if err := backend.EmitVerilog(design, svPath, opts); err != nil {
		return err
	}

	if *simulator == "" {
		fmt.Fprintf(os.Stdout, "Verilog written to %s\n", svPath)
		return nil
	}

	simulatorArgs := parseSimArgs(*simArgs)
	simulatorArgs = append(simulatorArgs, svPath)
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
		want, err := os.ReadFile(*expectPath)
		if err != nil {
			return fmt.Errorf("read expect file: %w", err)
		}
		got := stdoutBuf.Bytes()
		if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
			return fmt.Errorf("simulator output mismatch\nexpected:\n%s\nactual:\n%s", string(want), stdoutBuf.String())
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
