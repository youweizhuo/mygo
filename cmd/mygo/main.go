package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/tools/go/ssa"

	"mygo/internal/diag"
	"mygo/internal/frontend"
	"mygo/internal/ir"
	"mygo/internal/mlir"
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
	case "dump-ssa":
		return runDumpSSA(args[1:])
	case "dump-ir":
		return runDumpIR(args[1:])
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

	design, err := ir.BuildDesign(result.program, result.reporter)
	if err != nil {
		return err
	}

	switch *emit {
	case "mlir":
		return mlir.Emit(design, *output)
	case "verilog":
		return fmt.Errorf("verilog emission is not implemented yet")
	default:
		return fmt.Errorf("unknown emit format: %s", *emit)
	}

}

func printGlobalUsage() {
	fmt.Fprintf(os.Stderr, "MyGO compiler (phase 1 scaffold)\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  mygo <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  compile    Compile Go source to MLIR or Verilog (stub)\n")
	fmt.Fprintf(os.Stderr, "  dump-ssa   Load Go sources and dump SSA form\n")
	fmt.Fprintf(os.Stderr, "  dump-ir    Translate SSA into the MyGO hardware IR and dump it\n")
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

	design, err := ir.BuildDesign(result.program, result.reporter)
	if err != nil {
		return err
	}

	ir.Dump(design, os.Stdout)
	return nil
}

type frontendResult struct {
	reporter *diag.Reporter
	program  *ssa.Program
	ssaPkgs  []*ssa.Package
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
	}, nil
}
