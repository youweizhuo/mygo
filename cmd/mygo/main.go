package main

import (
	"flag"
	"fmt"
	"os"

	"mygo/internal/diag"
	"mygo/internal/frontend"
	"mygo/internal/ir"
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

	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("compile command requires a Go source file")
	}

	input := fs.Arg(0)

	fmt.Fprintf(os.Stdout, "mygo compile placeholder\n")
	fmt.Fprintf(os.Stdout, "  input: %s\n", input)
	fmt.Fprintf(os.Stdout, "  emit: %s\n", *emit)
	fmt.Fprintf(os.Stdout, "  output: %s\n", *output)
	fmt.Fprintf(os.Stdout, "  target: %s\n", *target)
	fmt.Fprintf(os.Stdout, "  diag-format: %s\n", *diagFormat)
	fmt.Fprintf(os.Stdout, "phase 1 implementation pending – this only configures the CLI\n")

	return nil
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

	reporter := diag.NewReporter(os.Stderr, *diagFormat)
	cfg := frontend.LoadConfig{
		Sources: fs.Args(),
	}

	pkgs, _, err := frontend.LoadPackages(cfg, reporter)
	if err != nil {
		return err
	}
	if reporter.HasErrors() {
		return fmt.Errorf("errors reported while loading packages")
	}

	_, ssaPkgs, err := frontend.BuildSSA(pkgs, reporter)
	if err != nil {
		return err
	}

	for _, pkg := range ssaPkgs {
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

	reporter := diag.NewReporter(os.Stderr, *diagFormat)
	cfg := frontend.LoadConfig{Sources: fs.Args()}

	pkgs, _, err := frontend.LoadPackages(cfg, reporter)
	if err != nil {
		return err
	}

	prog, _, err := frontend.BuildSSA(pkgs, reporter)
	if err != nil {
		return err
	}

	design, err := ir.BuildDesign(prog, reporter)
	if err != nil {
		return err
	}

	ir.Dump(design, os.Stdout)
	return nil
}
