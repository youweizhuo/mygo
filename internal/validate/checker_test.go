package validate

import (
	"bytes"
	"go/token"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"

	"mygo/internal/diag"
	"mygo/internal/frontend"
)

func TestValidateAllowsSimpleGoroutine(t *testing.T) {
	diagStr, err := runValidation(t, "ok_goroutine")
	if err != nil {
		t.Fatalf("expected success, got error %v with diagnostics %s", err, diagStr)
	}
	if diagStr != "" {
		t.Fatalf("expected no diagnostics, got %q", diagStr)
	}
}

func TestValidateRejectsGoroutineLoop(t *testing.T) {
	diagStr, err := runValidation(t, "bad_goroutine_loop")
	if err == nil {
		t.Fatalf("expected goroutine loop to fail")
	}
	if !strings.Contains(diagStr, "goroutines created inside loops") {
		t.Fatalf("expected loop diagnostic, got %q", diagStr)
	}
}

func TestValidateRejectsDynamicGoroutineTarget(t *testing.T) {
	diagStr, err := runValidation(t, "bad_goroutine_dynamic")
	if err == nil {
		t.Fatalf("expected dynamic goroutine target to fail")
	}
	if !strings.Contains(diagStr, "goroutine targets must be named functions") {
		t.Fatalf("expected named function diagnostic, got %q", diagStr)
	}
}

func TestValidateRejectsChannelCapacityIssues(t *testing.T) {
	diagStr, err := runValidation(t, "bad_channel_capacity")
	if err == nil {
		t.Fatalf("expected channel capacity violations to fail")
	}
	if !strings.Contains(diagStr, "compile-time constant") {
		t.Fatalf("expected constant capacity diagnostic, got %q", diagStr)
	}
	if !strings.Contains(diagStr, "positive constant") {
		t.Fatalf("expected positive capacity diagnostic, got %q", diagStr)
	}
}

func TestValidateRejectsChannelElementType(t *testing.T) {
	diagStr, err := runValidation(t, "bad_channel_type")
	if err == nil {
		t.Fatalf("expected channel element type to fail")
	}
	if !strings.Contains(diagStr, "channel element type") {
		t.Fatalf("expected element type diagnostic, got %q", diagStr)
	}
}

func TestValidateRejectsSelect(t *testing.T) {
	diagStr, err := runValidation(t, "bad_select")
	if err == nil {
		t.Fatalf("expected select to fail validation")
	}
	if !strings.Contains(diagStr, "select statements are not supported") {
		t.Fatalf("expected select diagnostic, got %q", diagStr)
	}
}

func TestValidateRejectsMaps(t *testing.T) {
	diagStr, err := runValidation(t, "bad_map")
	if err == nil {
		t.Fatalf("expected maps to fail validation")
	}
	if !strings.Contains(diagStr, "maps are not supported") {
		t.Fatalf("expected map diagnostic, got %q", diagStr)
	}
}

func TestValidateRejectsRecursion(t *testing.T) {
	diagStr, err := runValidation(t, "bad_recursion")
	if err == nil {
		t.Fatalf("expected recursion to fail validation")
	}
	if !strings.Contains(diagStr, "recursion is not supported") {
		t.Fatalf("expected recursion diagnostic, got %q", diagStr)
	}
}

func runValidation(t *testing.T, file string) (string, error) {
	t.Helper()
	prog, pkgs, astPkgs, fset := buildSSAProgram(t, file)
	var buf bytes.Buffer
	reporter := diag.NewReporter(&buf, "text")
	reporter.SetFileSet(fset)
	err := CheckProgram(prog, pkgs, astPkgs, reporter)
	return buf.String(), err
}

func buildSSAProgram(t *testing.T, file string) (*ssa.Program, []*ssa.Package, []*packages.Package, *token.FileSet) {
	t.Helper()
	cfg := frontend.LoadConfig{
		Sources: []string{filepath.Join("testdata", file, "main.go")},
	}
	loadReporter := diag.NewReporter(io.Discard, "text")
	pkgs, fset, err := frontend.LoadPackages(cfg, loadReporter)
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	if loadReporter.HasErrors() {
		t.Fatalf("package loading reported errors")
	}
	prog, ssaPkgs, err := frontend.BuildSSA(pkgs, loadReporter)
	if err != nil {
		t.Fatalf("build SSA: %v", err)
	}
	if loadReporter.HasErrors() {
		t.Fatalf("ssa construction reported errors")
	}
	return prog, ssaPkgs, pkgs, fset
}
