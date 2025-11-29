package backend

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mygo/internal/ir"
)

func TestEmitVerilogRunsTranslateOnly(t *testing.T) {
	requirePosix(t)

	design := testDesign()
	tmp := t.TempDir()

	translate := filepath.Join("testdata", "fakecirct", "translate.sh")

	out := filepath.Join(tmp, "out.sv")
	opts := Options{CIRCTTranslatePath: translate}
	res, err := EmitVerilog(design, out, opts)
	if err != nil {
		t.Fatalf("EmitVerilog failed: %v", err)
	}
	if res.MainPath != out {
		t.Fatalf("expected main path %s, got %s", out, res.MainPath)
	}
	if len(res.AuxPaths) != 0 {
		t.Fatalf("expected no aux files, got %v", res.AuxPaths)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "// verilog translator") {
		t.Fatalf("expected translator banner, got:\n%s", data)
	}
}

func TestEmitVerilogRunsOptWhenPipelineProvided(t *testing.T) {
	requirePosix(t)

	design := testDesign()
	tmp := t.TempDir()

	opt := filepath.Join("testdata", "fakecirct", "opt.sh")

	translate := writeScript(t, tmp, "translate.sh", `#!/bin/sh
set -e
INPUT=""
for arg in "$@"; do
  case "$arg" in
    --export-verilog)
      ;;
    *)
      INPUT="$arg"
      ;;
  esac
done
cat "$INPUT"
`)

	out := filepath.Join(tmp, "out.sv")
	opts := Options{
		CIRCTOptPath:       opt,
		CIRCTTranslatePath: translate,
		PassPipeline:       "pipeline-test",
	}
	if _, err := EmitVerilog(design, out, opts); err != nil {
		t.Fatalf("EmitVerilog failed: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "// opt:pipeline-test") {
		t.Fatalf("expected circt-opt banner, got:\n%s", data)
	}
}

func TestEmitVerilogDumpsFinalMLIR(t *testing.T) {
	requirePosix(t)

	design := testDesign()
	tmp := t.TempDir()

	translate := filepath.Join("testdata", "fakecirct", "translate.sh")
	opt := filepath.Join("testdata", "fakecirct", "opt.sh")
	dumpPath := filepath.Join(tmp, "mlir", "final.mlir")
	out := filepath.Join(tmp, "out.sv")
	opts := Options{
		CIRCTTranslatePath: translate,
		CIRCTOptPath:       opt,
		PassPipeline:       "pipeline-test",
		DumpMLIRPath:       dumpPath,
	}
	if _, err := EmitVerilog(design, out, opts); err != nil {
		t.Fatalf("EmitVerilog failed: %v", err)
	}
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read mlir dump: %v", err)
	}
	if !strings.Contains(string(data), "// opt:pipeline-test") {
		t.Fatalf("expected mlir dump to include opt output, got:\n%s", data)
	}
}

func TestEmitVerilogMissingTranslate(t *testing.T) {
	design := testDesign()
	opts := Options{CIRCTTranslatePath: filepath.Join(t.TempDir(), "missing")}
	out := filepath.Join(t.TempDir(), "out.sv")
	_, err := EmitVerilog(design, out, opts)
	if err == nil {
		t.Fatalf("expected error when circt-translate is missing")
	}
}

func TestEmitVerilogEmitsAuxiliaryFifoFile(t *testing.T) {
	requirePosix(t)
	design := testDesignWithChannel()
	tmp := t.TempDir()
	translate := writeScript(t, tmp, "translate.sh", `#!/bin/sh
set -e
cat <<'EOS'
module main();
endmodule
module mygo_fifo_i32_d1();
endmodule
EOS
`)
	fifoSrc := filepath.Join(tmp, "fifo_impl.sv")
	fifoBody := "// external fifo\nmodule mygo_fifo_i32_d1();\nendmodule\n"
	if err := os.WriteFile(fifoSrc, []byte(fifoBody), 0o644); err != nil {
		t.Fatalf("write fifo src: %v", err)
	}
	out := filepath.Join(tmp, "design.sv")
	res, err := EmitVerilog(design, out, Options{
		CIRCTTranslatePath: translate,
		FIFOSource:         fifoSrc,
	})
	if err != nil {
		t.Fatalf("EmitVerilog failed: %v", err)
	}
	if len(res.AuxPaths) != 1 {
		t.Fatalf("expected one aux file, got %v", res.AuxPaths)
	}
	mainData, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read main: %v", err)
	}
	if strings.Contains(string(mainData), "module mygo_fifo") {
		t.Fatalf("expected fifo module to be stripped:\n%s", string(mainData))
	}
	auxData, err := os.ReadFile(res.AuxPaths[0])
	if err != nil {
		t.Fatalf("read aux: %v", err)
	}
	if got := string(auxData); got != fifoBody {
		t.Fatalf("expected fifo implementation copy, got:\n%s", got)
	}
	expectedAux := strings.TrimSuffix(out, filepath.Ext(out)) + "_fifos.sv"
	if res.AuxPaths[0] != expectedAux {
		t.Fatalf("expected aux path %s, got %s", expectedAux, res.AuxPaths[0])
	}
	if _, err := os.Stat(expectedAux); err != nil {
		t.Fatalf("expected aux file to exist: %v", err)
	}
}

func TestEmitVerilogStripsAnnotatedFifoModules(t *testing.T) {
	requirePosix(t)
	design := testDesignWithChannel()
	tmp := t.TempDir()
	translate := writeScript(t, tmp, "translate.sh", `#!/bin/sh
set -e
cat <<'EOS'
module main();
endmodule : main
module helper();
endmodule // helper
module mygo_fifo_i32_d1();
endmodule : mygo_fifo_i32_d1
module sentinel();
endmodule // sentinel
EOS
`)
	fifoSrc := filepath.Join(tmp, "fifo_impl.sv")
	if err := os.WriteFile(fifoSrc, []byte("module mygo_fifo_i32_d1(); endmodule\n"), 0o644); err != nil {
		t.Fatalf("write fifo impl: %v", err)
	}
	out := filepath.Join(tmp, "design.sv")
	if _, err := EmitVerilog(design, out, Options{
		CIRCTTranslatePath: translate,
		FIFOSource:         fifoSrc,
	}); err != nil {
		t.Fatalf("EmitVerilog failed: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read design: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "mygo_fifo_i32_d1") {
		t.Fatalf("expected fifo module to be stripped:\n%s", text)
	}
	if strings.Contains(text, ": mygo_fifo_i32_d1") || strings.Contains(text, "// mygo_fifo_i32_d1") {
		t.Fatalf("expected fifo annotations to be removed:\n%s", text)
	}
}

func TestEmitVerilogCopiesFifoDirectory(t *testing.T) {
	requirePosix(t)
	design := testDesignWithChannel()
	tmp := t.TempDir()
	translate := writeScript(t, tmp, "translate.sh", `#!/bin/sh
set -e
cat <<'EOS'
module main();
endmodule
module mygo_fifo_i32_d1();
endmodule
EOS
`)
	srcDir := filepath.Join(tmp, "fifo_lib")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir fifo dir: %v", err)
	}
	fileA := filepath.Join(srcDir, "fifo_a.sv")
	fileB := filepath.Join(srcDir, "helpers", "helper.sv")
	if err := os.MkdirAll(filepath.Dir(fileB), 0o755); err != nil {
		t.Fatalf("mkdir helper dir: %v", err)
	}
	if err := os.WriteFile(fileA, []byte("module mygo_fifo_i32_d1(); endmodule\n"), 0o644); err != nil {
		t.Fatalf("write fifo a: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("// helper content\n"), 0o644); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	out := filepath.Join(tmp, "design.sv")
	res, err := EmitVerilog(design, out, Options{
		CIRCTTranslatePath: translate,
		FIFOSource:         srcDir,
	})
	if err != nil {
		t.Fatalf("EmitVerilog failed: %v", err)
	}
	if len(res.AuxPaths) != 2 {
		t.Fatalf("expected two aux files, got %v", res.AuxPaths)
	}
	for _, p := range res.AuxPaths {
		if !strings.HasPrefix(p, strings.TrimSuffix(out, filepath.Ext(out))+"_fifo_lib") {
			t.Fatalf("unexpected aux path %s", p)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected aux file %s to exist: %v", p, err)
		}
	}
}

func TestEmitVerilogErrorsWithoutFifoSource(t *testing.T) {
	requirePosix(t)
	design := testDesignWithChannel()
	tmp := t.TempDir()
	translate := writeScript(t, tmp, "translate.sh", `#!/bin/sh
set -e
cat <<'EOS'
module main();
endmodule
module mygo_fifo_i32_d1();
endmodule
EOS
`)
	out := filepath.Join(tmp, "design.sv")
	_, err := EmitVerilog(design, out, Options{CIRCTTranslatePath: translate})
	if err == nil || !strings.Contains(err.Error(), "fifo source") {
		t.Fatalf("expected fifo source error, got %v", err)
	}
}

func testDesign() *ir.Design {
	mod := &ir.Module{
		Name: "main",
		Ports: []ir.Port{
			{Name: "clk", Direction: ir.Input, Type: &ir.SignalType{Width: 1}},
			{Name: "rst", Direction: ir.Input, Type: &ir.SignalType{Width: 1}},
		},
		Signals:  map[string]*ir.Signal{},
		Channels: map[string]*ir.Channel{},
	}
	return &ir.Design{
		Modules:  []*ir.Module{mod},
		TopLevel: mod,
	}
}

func testDesignWithChannel() *ir.Design {
	ch := &ir.Channel{
		Name:  "t0",
		Type:  &ir.SignalType{Width: 32},
		Depth: 1,
	}
	mod := &ir.Module{
		Name:      "main",
		Ports:     []ir.Port{{Name: "clk", Direction: ir.Input, Type: &ir.SignalType{Width: 1}}, {Name: "rst", Direction: ir.Input, Type: &ir.SignalType{Width: 1}}},
		Signals:   map[string]*ir.Signal{},
		Channels:  map[string]*ir.Channel{"t0": ch},
		Processes: []*ir.Process{},
	}
	return &ir.Design{
		Modules:  []*ir.Module{mod},
		TopLevel: mod,
	}
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("tests require a POSIX shell")
	}
	return path
}

func requirePosix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tests require a POSIX shell")
	}
}
