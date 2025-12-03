package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mygo/internal/ir"
)

func TestEmitVerilogRunsExportVerilog(t *testing.T) {
	design := testDesign()
	tmp := t.TempDir()

	opt := touchFakeBinary(t, tmp)
	stubRunExport(t, func(binary, pipeline, loweringOptions, inputPath, mlirOutputPath, verilogOutputPath string) error {
		if binary != opt {
			return fmt.Errorf("unexpected binary %s", binary)
		}
		if pipeline != "" {
			return fmt.Errorf("expected empty pipeline, got %s", pipeline)
		}
		if err := copyFile(inputPath, mlirOutputPath); err != nil {
			return err
		}
		return os.WriteFile(verilogOutputPath, []byte("// circt-opt export\n"), 0o644)
	})

	out := filepath.Join(tmp, "out.sv")
	opts := Options{CIRCTOptPath: opt}
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
	if !strings.Contains(string(data), "// circt-opt export") {
		t.Fatalf("expected circt-opt export banner, got:\n%s", data)
	}
}

func TestEmitVerilogRunsOptWhenPipelineProvided(t *testing.T) {
	design := testDesign()
	tmp := t.TempDir()

	opt := touchFakeBinary(t, tmp)
	stubRunPipeline(t, func(binary, pipeline, inputPath, outputPath string) error {
		if binary != opt {
			return fmt.Errorf("unexpected binary %s", binary)
		}
		if pipeline != "pipeline-test" {
			return fmt.Errorf("expected pipeline-test, got %s", pipeline)
		}
		content, err := os.ReadFile(inputPath)
		if err != nil {
			return err
		}
		prefixed := append([]byte("// pipeline:"+pipeline+"\n"), content...)
		return os.WriteFile(outputPath, prefixed, 0o644)
	})
	stubRunExport(t, func(binary, pipeline, loweringOptions, inputPath, mlirOutputPath, verilogOutputPath string) error {
		if err := copyFile(inputPath, mlirOutputPath); err != nil {
			return err
		}
		return copyFile(inputPath, verilogOutputPath)
	})

	out := filepath.Join(tmp, "out.sv")
	opts := Options{
		CIRCTOptPath: opt,
		PassPipeline: "pipeline-test",
	}
	if _, err := EmitVerilog(design, out, opts); err != nil {
		t.Fatalf("EmitVerilog failed: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "// pipeline:pipeline-test") {
		t.Fatalf("expected pipeline banner, got:\n%s", data)
	}
}

func TestEmitVerilogDumpsFinalMLIR(t *testing.T) {
	design := testDesign()
	tmp := t.TempDir()

	opt := touchFakeBinary(t, tmp)
	dumpPath := filepath.Join(tmp, "mlir", "final.mlir")
	out := filepath.Join(tmp, "out.sv")
	stubRunPipeline(t, func(binary, pipeline, inputPath, outputPath string) error {
		content, err := os.ReadFile(inputPath)
		if err != nil {
			return err
		}
		prefixed := append([]byte("// opt:pipeline-test\n"), content...)
		return os.WriteFile(outputPath, prefixed, 0o644)
	})
	stubRunExport(t, func(binary, pipeline, loweringOptions, inputPath, mlirOutputPath, verilogOutputPath string) error {
		if err := copyFile(inputPath, mlirOutputPath); err != nil {
			return err
		}
		return copyFile(inputPath, verilogOutputPath)
	})
	opts := Options{
		CIRCTOptPath: opt,
		PassPipeline: "pipeline-test",
		DumpMLIRPath: dumpPath,
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

func TestEmitVerilogMissingCirctOpt(t *testing.T) {
	design := testDesign()
	opts := Options{CIRCTOptPath: filepath.Join(t.TempDir(), "missing")}
	out := filepath.Join(t.TempDir(), "out.sv")
	_, err := EmitVerilog(design, out, opts)
	if err == nil {
		t.Fatalf("expected error when circt-opt is missing")
	}
}

func TestEmitVerilogEmitsAuxiliaryFifoFile(t *testing.T) {
	design := testDesignWithChannel()
	tmp := t.TempDir()
	opt := touchFakeBinary(t, tmp)
	stubRunExport(t, func(binary, pipeline, loweringOptions, inputPath, mlirOutputPath, verilogOutputPath string) error {
		if err := copyFile(inputPath, mlirOutputPath); err != nil {
			return err
		}
		return os.WriteFile(verilogOutputPath, []byte(readBackendTestdata(t, "verilog_with_fifo.sv")), 0o644)
	})
	fifoSrc := filepath.Join(tmp, "fifo_impl.sv")
	fifoBody := readBackendTestdata(t, "fifo_impl_external.sv")
	if err := os.WriteFile(fifoSrc, []byte(fifoBody), 0o644); err != nil {
		t.Fatalf("write fifo src: %v", err)
	}
	out := filepath.Join(tmp, "design.sv")
	res, err := EmitVerilog(design, out, Options{
		CIRCTOptPath: opt,
		FIFOSource:   fifoSrc,
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

func TestEmitVerilogGeneratesFifoWrappers(t *testing.T) {
	design := testDesignWithChannel()
	tmp := t.TempDir()
	opt := touchFakeBinary(t, tmp)
	stubRunExport(t, func(binary, pipeline, loweringOptions, inputPath, mlirOutputPath, verilogOutputPath string) error {
		if err := copyFile(inputPath, mlirOutputPath); err != nil {
			return err
		}
		return os.WriteFile(verilogOutputPath, []byte(readBackendTestdata(t, "verilog_with_fifo.sv")), 0o644)
	})
	fifoSrc := filepath.Join(tmp, "fifo_template.sv")
	if err := os.WriteFile(fifoSrc, []byte(readBackendTestdata(t, "fifo_template.sv")), 0o644); err != nil {
		t.Fatalf("write fifo template: %v", err)
	}
	out := filepath.Join(tmp, "design.sv")
	res, err := EmitVerilog(design, out, Options{
		CIRCTOptPath: opt,
		FIFOSource:   fifoSrc,
	})
	if err != nil {
		t.Fatalf("EmitVerilog failed: %v", err)
	}
	if len(res.AuxPaths) != 1 {
		t.Fatalf("expected one aux file, got %v", res.AuxPaths)
	}
	data, err := os.ReadFile(res.AuxPaths[0])
	if err != nil {
		t.Fatalf("read aux: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "mygo:fifo_template") {
		t.Fatalf("expected fifo template body to be copied:\n%s", text)
	}
	if !strings.Contains(text, "module mygo_fifo_i32_d1") {
		t.Fatalf("expected fifo wrapper to be generated:\n%s", text)
	}
}

func TestEmitVerilogStripsAnnotatedFifoModules(t *testing.T) {
	design := testDesignWithChannel()
	tmp := t.TempDir()
	opt := touchFakeBinary(t, tmp)
	stubRunExport(t, func(binary, pipeline, loweringOptions, inputPath, mlirOutputPath, verilogOutputPath string) error {
		if err := copyFile(inputPath, mlirOutputPath); err != nil {
			return err
		}
		return os.WriteFile(verilogOutputPath, []byte(readBackendTestdata(t, "verilog_with_annotations.sv")), 0o644)
	})
	fifoSrc := filepath.Join(tmp, "fifo_impl.sv")
	if err := os.WriteFile(fifoSrc, []byte(readBackendTestdata(t, "fifo_impl_basic.sv")), 0o644); err != nil {
		t.Fatalf("write fifo impl: %v", err)
	}
	out := filepath.Join(tmp, "design.sv")
	if _, err := EmitVerilog(design, out, Options{
		CIRCTOptPath: opt,
		FIFOSource:   fifoSrc,
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
	design := testDesignWithChannel()
	tmp := t.TempDir()
	opt := touchFakeBinary(t, tmp)
	stubRunExport(t, func(binary, pipeline, loweringOptions, inputPath, mlirOutputPath, verilogOutputPath string) error {
		if err := copyFile(inputPath, mlirOutputPath); err != nil {
			return err
		}
		return os.WriteFile(verilogOutputPath, []byte(readBackendTestdata(t, "verilog_with_fifo.sv")), 0o644)
	})
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
		CIRCTOptPath: opt,
		FIFOSource:   srcDir,
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
	design := testDesignWithChannel()
	tmp := t.TempDir()
	opt := touchFakeBinary(t, tmp)
	stubRunExport(t, func(binary, pipeline, loweringOptions, inputPath, mlirOutputPath, verilogOutputPath string) error {
		if err := copyFile(inputPath, mlirOutputPath); err != nil {
			return err
		}
		return os.WriteFile(verilogOutputPath, []byte(readBackendTestdata(t, "verilog_with_fifo.sv")), 0o644)
	})
	out := filepath.Join(tmp, "design.sv")
	_, err := EmitVerilog(design, out, Options{CIRCTOptPath: opt})
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

func stubRunPipeline(t *testing.T, fn func(binary, pipeline, inputPath, outputPath string) error) {
	t.Helper()
	prev := runPipeline
	runPipeline = fn
	t.Cleanup(func() { runPipeline = prev })
}

func stubRunExport(t *testing.T, fn func(binary, pipeline, loweringOptions, inputPath, mlirOutputPath, verilogOutputPath string) error) {
	t.Helper()
	prev := runExport
	runExport = fn
	t.Cleanup(func() { runExport = prev })
}

func touchFakeBinary(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "circt-opt")
	if err := os.WriteFile(path, []byte{}, 0o755); err != nil {
		t.Fatalf("touch binary: %v", err)
	}
	return path
}

func backendTestdataPath(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("testdata %s: %v", name, err)
	}
	return path
}

func readBackendTestdata(t *testing.T, name string) string {
	t.Helper()
	path := backendTestdataPath(t, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read testdata %s: %v", name, err)
	}
	return string(data)
}
