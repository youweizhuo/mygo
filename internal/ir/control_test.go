package ir

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"mygo/internal/diag"
	"mygo/internal/frontend"
)

const branchProgram = `
package main

func sink(v int32) {}

func main() {
    var a, b, out int32
    a = 5
    b = 7
    if a < b {
        out = a + 1
    } else {
        out = b - 1
    }
    sink(out)
}
`

func TestControlFlowMuxLowering(t *testing.T) {
	design := buildDesignFromSource(t, branchProgram)
	if design == nil || design.TopLevel == nil {
		t.Fatalf("expected design with top-level module")
	}
	muxCount := 0
	cmpCount := 0
	for _, proc := range design.TopLevel.Processes {
		for _, block := range proc.Blocks {
			for _, op := range block.Ops {
				switch op.(type) {
				case *MuxOperation:
					muxCount++
				case *CompareOperation:
					cmpCount++
				}
			}
		}
	}
	if muxCount == 0 {
		t.Fatalf("expected at least one mux operation in lowered IR")
	}
	if cmpCount == 0 {
		t.Fatalf("expected a compare operation for the branch predicate")
	}
}

func TestControlFlowBranchMetadata(t *testing.T) {
	design := buildDesignFromSource(t, branchProgram)
	if design == nil || design.TopLevel == nil {
		t.Fatalf("expected design")
	}
	var branch *BranchTerminator
	for _, proc := range design.TopLevel.Processes {
		for _, block := range proc.Blocks {
			if term, ok := block.Terminator.(*BranchTerminator); ok {
				branch = term
				break
			}
		}
	}
	if branch == nil {
		t.Fatalf("expected a branch terminator in control-flow graph")
	}
	if branch.Cond == nil {
		t.Fatalf("branch terminator missing predicate signal")
	}
	if branch.True == nil || branch.False == nil {
		t.Fatalf("branch terminator missing successors")
	}
}

func buildDesignFromSource(t *testing.T, source string) *Design {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	goMod := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(goMod, []byte("module testcase\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	reporter := diag.NewReporter(io.Discard, "text")
	cfg := frontend.LoadConfig{Sources: []string{file}}
	pkgs, _, err := frontend.LoadPackages(cfg, reporter)
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	prog, _, err := frontend.BuildSSA(pkgs, reporter)
	if err != nil {
		t.Fatalf("build ssa: %v", err)
	}
	design, err := BuildDesign(prog, reporter)
	if err != nil {
		t.Fatalf("build design: %v", err)
	}
	return design
}
