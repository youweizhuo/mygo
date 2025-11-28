package backend

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"mygo/internal/ir"
	"mygo/internal/mlir"
)

// Options configures how the CIRCT backend is invoked.
type Options struct {
	// CIRCTOptPath optionally overrides the circt-opt binary. When empty the
	// backend looks it up on PATH if needed.
	CIRCTOptPath string
	// CIRCTTranslatePath optionally overrides the circt-translate binary. When
	// empty the backend looks it up on PATH.
	CIRCTTranslatePath string
	// PassPipeline holds the circt-opt --pass-pipeline string (empty skips
	// circt-opt unless CIRCTOptPath is explicitly set).
	PassPipeline string
	// DumpMLIRPath writes the MLIR handed to CIRCT to the provided path when
	// non-empty.
	DumpMLIRPath string
	// KeepTemps preserves the intermediate directory on disk for debugging.
	KeepTemps bool
	// FIFOSource points to a user-provided FIFO implementation that will be
	// copied next to the emitted Verilog when channels are present.
	FIFOSource string
}

// Result lists the artifacts produced during Verilog emission.
type Result struct {
	MainPath string
	AuxPaths []string
}

// EmitVerilog lowers the design to MLIR, optionally runs circt-opt, and invokes
// circt-translate --export-verilog to produce SystemVerilog at outputPath.
// When FIFOs are present, auxiliary files are produced as well and returned via
// Result.AuxPaths.
func EmitVerilog(design *ir.Design, outputPath string, opts Options) (Result, error) {
	if design == nil {
		return Result{}, fmt.Errorf("backend: design is nil")
	}
	if outputPath == "" || outputPath == "-" {
		return Result{}, fmt.Errorf("backend: verilog emission requires -o when auxiliary FIFO sources are generated")
	}

	fifoInfos := collectFifoDescriptors(design)

	translatePath, err := resolveBinary(opts.CIRCTTranslatePath, "circt-translate")
	if err != nil {
		return Result{}, fmt.Errorf("backend: resolve circt-translate: %w", err)
	}

	var optPath string
	if needsOpt(opts) {
		if optPath, err = resolveBinary(opts.CIRCTOptPath, "circt-opt"); err != nil {
			return Result{}, fmt.Errorf("backend: resolve circt-opt: %w", err)
		}
	}

	tempDir, err := os.MkdirTemp("", "mygo-circt-*")
	if err != nil {
		return Result{}, fmt.Errorf("backend: create temp dir: %w", err)
	}
	if !opts.KeepTemps {
		defer os.RemoveAll(tempDir)
	}

	mlirPath := opts.DumpMLIRPath
	if mlirPath == "" {
		mlirPath = filepath.Join(tempDir, "design.mlir")
	} else if err := os.MkdirAll(filepath.Dir(mlirPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("backend: create circt-mlir dir: %w", err)
	}

	if err := mlir.Emit(design, mlirPath); err != nil {
		return Result{}, fmt.Errorf("backend: emit mlir: %w", err)
	}

	currentInput := mlirPath
	if needsOpt(opts) {
		optOutput := filepath.Join(tempDir, "design.opt.mlir")
		if err := runCirctOpt(optPath, opts.PassPipeline, currentInput, optOutput); err != nil {
			return Result{}, err
		}
		currentInput = optOutput
	}

	if err := runCirctTranslate(translatePath, currentInput, outputPath); err != nil {
		return Result{}, err
	}

	auxPath, err := stripAndWriteFifos(outputPath, fifoInfos, opts.FIFOSource)
	if err != nil {
		return Result{}, err
	}
	res := Result{MainPath: outputPath}
	if auxPath != "" {
		res.AuxPaths = append(res.AuxPaths, auxPath)
	}
	return res, nil
}

func needsOpt(opts Options) bool {
	return opts.PassPipeline != "" || opts.CIRCTOptPath != ""
}

func runCirctOpt(binary, pipeline, inputPath, outputPath string) error {
	args := []string{inputPath, "-o", outputPath}
	if pipeline != "" {
		args = append(args, "--pass-pipeline="+pipeline)
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backend: circt-opt failed: %w", err)
	}
	return nil
}

func runCirctTranslate(binary, inputPath, outputPath string) error {
	args := []string{"--export-verilog", inputPath}
	cmd := exec.Command(binary, args...)
	cmd.Stderr = os.Stderr

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("backend: create verilog output dir: %w", err)
	}
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("backend: create verilog output file: %w", err)
	}
	defer outFile.Close()
	cmd.Stdout = outFile

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backend: circt-translate failed: %w", err)
	}
	return nil
}

func resolveBinary(explicit, fallback string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}
	path, err := exec.LookPath(fallback)
	if err != nil {
		return "", err
	}
	return path, nil
}

type fifoDescriptor struct {
	name  string
	width int
	depth int
}

func collectFifoDescriptors(design *ir.Design) []fifoDescriptor {
	seen := make(map[string]fifoDescriptor)
	if design == nil {
		return nil
	}
	for _, module := range design.Modules {
		if module == nil {
			continue
		}
		for _, ch := range module.Channels {
			if ch == nil {
				continue
			}
			width := signalWidth(ch.Type)
			depth := ch.Depth
			if depth <= 0 {
				depth = 1
			}
			elem := signalTypeString(ch.Type)
			name := fifoModuleName(elem, depth)
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = fifoDescriptor{
				name:  name,
				width: width,
				depth: depth,
			}
		}
	}
	result := make([]fifoDescriptor, 0, len(seen))
	for _, desc := range seen {
		result = append(result, desc)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].name < result[j].name
	})
	return result
}

func stripAndWriteFifos(mainPath string, fifos []fifoDescriptor, fifoSource string) (string, error) {
	if len(fifos) == 0 {
		return "", nil
	}
	if fifoSource == "" {
		return "", fmt.Errorf("backend: fifo source required when channels are present")
	}
	data, err := os.ReadFile(mainPath)
	if err != nil {
		return "", fmt.Errorf("backend: read verilog output: %w", err)
	}
	updated := string(data)
	for _, fifo := range fifos {
		var ok bool
		updated, ok = removeModuleBlock(updated, fifo.name)
		if !ok {
			return "", fmt.Errorf("backend: module %s not found in generated Verilog", fifo.name)
		}
	}
	if err := os.WriteFile(mainPath, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("backend: update main verilog: %w", err)
	}

	auxPath := strings.TrimSuffix(mainPath, filepath.Ext(mainPath)) + "_fifos.sv"
	src, err := os.ReadFile(fifoSource)
	if err != nil {
		return "", fmt.Errorf("backend: read fifo source: %w", err)
	}
	if err := os.WriteFile(auxPath, src, 0o644); err != nil {
		return "", fmt.Errorf("backend: write fifo sources: %w", err)
	}
	return auxPath, nil
}

func removeModuleBlock(content, moduleName string) (string, bool) {
	marker := "module " + moduleName
	start := strings.Index(content, marker)
	if start == -1 {
		return content, false
	}
	tail := content[start:]
	endIdx := strings.Index(tail, "endmodule")
	if endIdx == -1 {
		return content, false
	}
	end := start + endIdx + len("endmodule")
	for end < len(content) && (content[end] == '\n' || content[end] == '\r') {
		end++
	}
	return content[:start] + content[end:], true
}

func fifoModuleName(elemType string, depth int) string {
	return fmt.Sprintf("mygo_fifo_%s_d%d", sanitize(elemType), depth)
}

func signalWidth(t *ir.SignalType) int {
	if t == nil || t.Width <= 0 {
		return 1
	}
	return t.Width
}

func signalTypeString(t *ir.SignalType) string {
	return fmt.Sprintf("i%d", signalWidth(t))
}

func sanitize(name string) string {
	if name == "" {
		return "unnamed"
	}
	var b strings.Builder
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9' && i > 0) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
