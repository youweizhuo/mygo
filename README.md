# MyGO: Go-Subset Hardware DSL

**Development Specification v0.1**

---

## Table of Contents

1. [For Students: Getting Started](#1-for-students-getting-started)
2. [Project Overview](#2-project-overview)
3. [Background & Key Concepts](#3-background--key-concepts)
4. [Architecture](#4-architecture)
5. [Development Phases](#5-development-phases)
6. [Implementation Details](#6-implementation-details)
7. [Testing Strategy](#7-testing-strategy)
8. [Coding Standards](#8-coding-standards)
9. [References & Resources](#9-references--resources)

---

## 1. For Students: Getting Started

### 1.1 Prerequisites

**Required Knowledge:**
- Compiler fundamentals (lexing, parsing, AST, type checking)
- Basic understanding of control flow graphs
- Familiarity with any programming language (C, Java, Python, etc.)

**What You'll Learn:**
- Go programming language
- MLIR (Multi-Level Intermediate Representation)
- CIRCT (Circuit IR Compilers and Tools)
- Hardware description concepts
- SSA (Static Single Assignment) form

### 1.2 Environment Setup

**System Requirements:**
- Linux (Ubuntu 20.04+ recommended)
- 8GB RAM minimum, 16GB recommended
- 20GB free disk space

**Installation Steps:**

```bash
# 1. Install Go 1.22+
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# 2. Clone the project
git clone https://github.com/your-org/mygo.git
cd mygo

# 3. Install dependencies
go mod download

# 4. Build MLIR/CIRCT (detailed instructions in docs/circt-setup.md)
# This step takes 30-60 minutes
./scripts/install-circt.sh

# 5. Verify installation
go test ./...
./scripts/verify-environment.sh
```

### 1.3 Recommended Learning Path

**Week 1-2: Go Language Basics**
- Complete [A Tour of Go](https://go.dev/tour/)
- Read `internal/frontend` code to understand Go's AST and type system
- Exercise: Write a simple Go program that prints variable types

**Week 3-4: Understanding SSA**
- Study `go/ssa` package documentation
- Run `go build -gcflags="-S"` on simple programs to see SSA output
- Exercise: Manually convert a simple function to SSA form

**Week 5-6: MLIR Basics**
- Read [MLIR Language Reference](https://mlir.llvm.org/docs/LangRef/)
- Study CIRCT's `hw`, `comb`, `seq` dialects
- Exercise: Write simple MLIR by hand and verify with `mlir-opt`

**Week 7+: Project Development**
- Follow the development phases below
- Start with Phase 1 (minimal viable compiler)

---

## 2. Project Overview

### 2.1 Objectives

**Primary Goal:**
Create a compiler that translates a subset of Go syntax into hardware RTL (Register Transfer Level) code, enabling software engineers to describe hardware using familiar Go syntax.

**Key Features:**
1. Accept a controlled subset of Go syntax
2. Leverage LLGo's frontend for parsing and SSA generation
3. Translate to CIRCT-compatible MLIR
4. Generate synthesizable Verilog/SystemVerilog
5. Provide clear error messages for unsupported constructs

### 2.2 Example: Input Syntax (MyGO)

Based on the [argo2verilog](https://github.com/rmartin101/argo2verilog) syntax:

```go
// simple.go - Basic arithmetic module
package main

import "fmt"

func main() {
    var i, dead, l int32
    var j int16
    var k int64

    dead = 3
    l = dead
    i = 1
    j = 2
    k = i + j

    fmt.Printf("The result is small: %d\n", k)
}
```

**Supported Constructs (Subset):**
- Variable declarations: `var x, y int32`
- Assignment: `x = y + 1`
- Arithmetic operators: `+`, `-`, `*` (division requires special handling)
- Integer types: `int8`, `int16`, `int32`, `int64`, `uint8`, `uint16`, `uint32`, `uint64`
- Basic control flow: `if`, `for` (with compile-time bounds)
- Function calls to `fmt.Printf` (for debugging/simulation only)

**Unsupported (will error):**
- Goroutines, channels
- Interfaces, reflection
- Recursion
- Unbounded loops
- Maps, slices (initially; fixed arrays may be supported later)
- Function pointers

### 2.3 Compilation Pipeline

```
┌─────────────┐
│  simple.go  │ (Input: Go source file)
└──────┬──────┘
       │
       ▼
┌─────────────────┐
│  Go Parser      │ (LLGo frontend)
│  + Type Checker │
└──────┬──────────┘
       │
       ▼
┌─────────────┐
│   Go AST    │ (Abstract Syntax Tree)
└──────┬──────┘
       │
       ▼
┌─────────────────┐
│ DSL Desugar     │ (Transform hardware annotations)
└──────┬──────────┘
       │
       ▼
┌─────────────┐
│   SSA IR    │ (Static Single Assignment)
└──────┬──────┘
       │
       ▼
┌─────────────────┐
│ SSA Validator   │ (Check for unsupported constructs)
└──────┬──────────┘
       │
       ▼
┌─────────────────┐
│ Hardware IR     │ (MyGO internal representation)
│ Builder         │
└──────┬──────────┘
       │
       ▼
┌─────────────────┐
│ Optimization    │ (Constant folding, width inference)
│ Passes          │
└──────┬──────────┘
       │
       ▼
┌─────────────────┐
│ MLIR Emission   │ (CIRCT dialects: hw, comb, seq)
└──────┬──────────┘
       │
       ▼
┌─────────────┐
│  .mlir file │
└──────┬──────┘
       │
       ▼
┌─────────────────┐
│  circt-opt      │ (External tool)
│  + verilog gen  │
└──────┬──────────┘
       │
       ▼
┌─────────────┐
│ Verilog RTL │ (Final hardware description)
└─────────────┘
```

---

## 3. Background & Key Concepts

### 3.1 SSA (Static Single Assignment)

**What is SSA?**
An intermediate representation where each variable is assigned exactly once, making dataflow analysis easier.

**Example Transformation:**

```go
// Original Go code
x := 10
x = x + 5
x = x * 2
```

**SSA Form:**
```
t0 = 10
t1 = t0 + 5
t2 = t1 * 2
```

**Why SSA for Hardware?**
- Makes dataflow explicit (easier to map to hardware signals)
- Simplifies optimization passes
- Each SSA value can map to a wire or register

**Go SSA Package:**
The `golang.org/x/tools/go/ssa` package (used by LLGo) provides:
- `ssa.Program`: Collection of packages
- `ssa.Function`: Control flow graph with basic blocks
- `ssa.BasicBlock`: Sequence of instructions
- `ssa.Instruction`: Individual operations (BinOp, Store, Load, etc.)

### 3.2 MLIR & CIRCT

**MLIR (Multi-Level Intermediate Representation):**
- A compiler infrastructure project from LLVM
- Allows multiple levels of abstraction in one IR
- Extensible through "dialects" (domain-specific operations)

**CIRCT (Circuit IR Compilers and Tools):**
- MLIR dialects for hardware design
- Key dialects:
 - `hw`: Structural hardware (modules, ports)
 - `comb`: Combinational logic (AND, OR, ADD, etc.)
 - `seq`: Sequential logic (registers, clocks)
 - `sv`: SystemVerilog-specific constructs

**Example MLIR (hw dialect):**

```mlir
hw.module @simple(%clk: i1, %rst: i1) -> (out: i32) {
  %c1 = hw.constant 1 : i32
  %c2 = hw.constant 2 : i32
  %sum = comb.add %c1, %c2 : i32
  hw.output %sum : i32
}
```

### 3.3 Hardware Description Concepts

**For Software Engineers:**

| Software Concept | Hardware Equivalent |
|-----------------|-------------------|
| Variable | Wire or Register |
| Assignment (one-time) | Wire connection |
| Assignment (in loop) | Register update |
| `if` statement | Multiplexer (MUX) |
| `for` loop | State machine or unrolled logic |
| Function call | Sub-module instantiation |

**Key Differences:**
- **Parallelism**: All hardware "executes" simultaneously
- **No dynamic allocation**: All resources fixed at synthesis time
- **Clock-driven**: Sequential logic updates on clock edges
- **Bit widths matter**: Must track width of every signal

---

## 4. Architecture

### 4.1 Project Structure

```
mygo/
├── cmd/
│   └── mygo/              # CLI entry point
│       └── main.go
├── internal/
│   ├── frontend/          # Go parsing & SSA generation
│   │   ├── loader.go      # Package loading wrapper
│   │   └── ssa.go         # SSA builder
│   ├── desugar/           # DSL-specific AST transforms
│   │   ├── annotations.go # Process @hardware, @pipeline
│   │   └── pipeline.go    # Pipeline block rewriting
│   ├── validate/          # SSA validation
│   │   └── checker.go     # Verify supported constructs
│   ├── ir/                # Hardware IR
│   │   ├── design.go      # Top-level design types
│   │   ├── module.go      # Module representation
│   │   ├── signal.go      # Signal/wire/register types
│   │   └── builder.go     # SSA → IR translation
│   ├── passes/            # Optimization passes
│   │   ├── pass.go        # Pass interface
│   │   ├── constfold.go   # Constant folding
│   │   └── widthinfer.go  # Bit-width inference
│   ├── mlir/              # MLIR emission
│   │   ├── emitter.go     # IR → MLIR translation
│   │   └── printer.go     # MLIR text output
│   └── diag/              # Diagnostics
│       └── reporter.go    # Error reporting
├── third_party/
│   └── llgo/              # Vendored LLGo components
│       └── packages/      # Package loading utilities
├── test/
│   ├── e2e/               # End-to-end test cases
│   │   ├── simple.go
│   │   ├── simple.mlir    # Expected output
│   │   └── simple.sv      # Expected Verilog
│   └── unit/              # Unit tests
├── docs/
│   ├── circt-setup.md     # CIRCT installation guide
│   ├── dsl-spec.md        # DSL language specification
│   └── architecture.md    # Detailed architecture
├── scripts/
│   ├── install-circt.sh
│   └── verify-environment.sh
├── go.mod
├── go.sum
└── README.md
```

### 4.2 Component Overview

#### 4.2.1 CLI Layer (`cmd/mygo`)

**Commands:**

```bash
# Compile Go to MLIR
mygo compile simple.go --emit=mlir -o simple.mlir

# Compile Go to Verilog
mygo compile simple.go --emit=verilog -o simple.sv

# Dump SSA for debugging
mygo dump-ssa simple.go

# Lint for unsupported constructs
mygo lint simple.go

# Dump intermediate IR
mygo dump-ir simple.go
```

**Global Flags:**
- `--target=<name>`: Target function/module (default: `main`)
- `--clock=<signal>`: Clock signal name (default: `clk`)
- `--reset=<signal>`: Reset signal name (default: `rst`)
- `--emit=<format>`: Output format (`mlir`, `verilog`)
- `--diag-format=<format>`: Diagnostic format (`text`, `json`)
- `--opt-passes=<list>`: Comma-separated optimization passes

**Implementation (`cmd/mygo/main.go`):**

```go
package main

import (
    "flag"
    "fmt"
    "os"

    "mygo/internal/frontend"
    "mygo/internal/validate"
    "mygo/internal/ir"
    "mygo/internal/passes"
    "mygo/internal/mlir"
    "mygo/internal/diag"
)

func main() {
    // Parse command-line flags
    emitFlag := flag.String("emit", "mlir", "output format")
    outputFlag := flag.String("o", "", "output file")
    flag.Parse()

    if flag.NArg() < 1 {
        fmt.Fprintf(os.Stderr, "Usage: mygo <file.go>\n")
        os.Exit(1)
    }

    inputFile := flag.Arg(0)
    reporter := diag.NewReporter(os.Stderr, "text")

    // Phase 1: Load and parse
    pkgs, err := frontend.LoadPackages([]string{inputFile}, reporter)
    if err != nil {
        reporter.Fatal(err)
    }

    // Phase 2: Build SSA
    prog, err := frontend.BuildSSA(pkgs, reporter)
    if err != nil {
        reporter.Fatal(err)
    }

    // Phase 3: Validate SSA
    if err := validate.CheckSupported(prog, reporter); err != nil {
        reporter.Fatal(err)
    }

    // Phase 4: Translate to Hardware IR
    design, err := ir.BuildDesign(prog, reporter)
    if err != nil {
        reporter.Fatal(err)
    }

    // Phase 5: Run optimization passes
    passMgr := passes.NewManager()
    passMgr.Add(passes.NewConstantFolder())
    passMgr.Add(passes.NewWidthInference())
    if err := passMgr.Run(design); err != nil {
        reporter.Fatal(err)
    }

    // Phase 6: Emit output
    switch *emitFlag {
    case "mlir":
        if err := mlir.EmitMLIR(design, *outputFlag, reporter); err != nil {
            reporter.Fatal(err)
        }
    case "verilog":
        // Call external circt-opt tool
        if err := mlir.EmitVerilog(design, *outputFlag, reporter); err != nil {
            reporter.Fatal(err)
        }
    default:
        reporter.Fatal(fmt.Errorf("unknown emit format: %s", *emitFlag))
    }
}
```

#### 4.2.2 Frontend Wrapper (`internal/frontend`)

**Responsibilities:**
1. Load Go packages using LLGo's package loader
2. Build SSA representation
3. Handle build tags and module caching

**Key Types:**

```go
// loader.go
type LoadConfig struct {
    Sources   []string          // Input .go files
    GOROOT    string           // Go root directory
    GOPATH    string           // Go path
    BuildTags []string         // Build constraints
}

func LoadPackages(sources []string, reporter *diag.Reporter) ([]*packages.Package, error)
```

```go
// ssa.go
func BuildSSA(pkgs []*packages.Package, reporter *diag.Reporter) (*ssa.Program, error)
```

**Implementation Notes:**
- Use `packages.Load` with mode: `NeedSyntax | NeedTypes | NeedTypesInfo | NeedCompiledGoFiles`
- Hardcode `GOOS=linux` and `GOARCH=amd64` to avoid cross-platform issues
- Apply deduplication to avoid re-parsing shared dependencies

#### 4.2.3 DSL Desugaring (`internal/desugar`)

**Purpose:**
Transform DSL-specific syntax into standard Go before SSA generation.

**Example Annotations (future extension):**

```go
//@mygo:hardware
func Adder(a, b int32) int32 {
    return a + b
}

//@mygo:pipeline(stages=3)
func PipelinedMul(x, y int32) int32 {
    stage1 := x * y
    //@mygo:stage(1)
    stage2 := stage1 + 1
    //@mygo:stage(2)
    return stage2
}
```

**For Phase 1 (simple.go):**
No desugaring needed; this component can be a placeholder.

#### 4.2.4 SSA Validation (`internal/validate`)

**Checker Implementation:**

```go
// checker.go
package validate

import (
    "fmt"
    "golang.org/x/tools/go/ssa"
    "mygo/internal/diag"
)

// Allowed instruction types
var allowedInstructions = map[string]bool{
    "*ssa.Alloc":       true,
    "*ssa.Store":       true,
    "*ssa.UnOp":        true,
    "*ssa.BinOp":       true,
    "*ssa.Const":       true,
    "*ssa.ChangeType":  true,
    "*ssa.Convert":     true,
    "*ssa.Return":      true,
    "*ssa.If":          true,
    "*ssa.Jump":        true,
    "*ssa.Call":        true,  // Limited to fmt.Printf for now
}

func CheckSupported(prog *ssa.Program, reporter *diag.Reporter) error {
    for _, pkg := range prog.AllPackages() {
        for _, member := range pkg.Members {
            fn, ok := member.(*ssa.Function)
            if !ok {
                continue
            }

            for _, block := range fn.Blocks {
                for _, instr := range block.Instrs {
                    instrType := fmt.Sprintf("%T", instr)

                    if !allowedInstructions[instrType] {
                        reporter.Error(
                            instr.Pos(),
                            fmt.Sprintf("unsupported instruction: %s", instrType),
                        )
                        return fmt.Errorf("validation failed")
                    }

                    // Special checks
                    if call, ok := instr.(*ssa.Call); ok {
                        if err := checkCall(call, reporter); err != nil {
                            return err
                        }
                    }
                }
            }
        }
    }
    return nil
}

func checkCall(call *ssa.Call, reporter *diag.Reporter) error {
    // Only allow fmt.Printf calls
    if builtin, ok := call.Call.Value.(*ssa.Builtin); ok {
        reporter.Error(call.Pos(), fmt.Sprintf("builtin %s not supported", builtin.Name()))
        return fmt.Errorf("unsupported builtin")
    }

    // Check for goroutines
    if call.Common().IsInvoke() {
        reporter.Error(call.Pos(), "interface method calls not supported")
        return fmt.Errorf("unsupported invoke")
    }

    return nil
}
```

#### 4.2.5 Hardware IR Builder (`internal/ir`)

**Core Types:**

```go
// design.go
type Design struct {
    Modules  []*Module
    TopLevel *Module  // Entry point module
}

// module.go
type Module struct {
    Name      string
    Ports     []Port
    Signals   map[string]*Signal
    Processes []*Process
    Source    token.Pos  // For error reporting
}

type Port struct {
    Name      string
    Direction PortDirection  // Input, Output, InOut
    Type      *SignalType
}

type PortDirection int
const (
    Input PortDirection = iota
    Output
    InOut
)

// signal.go
type Signal struct {
    Name   string
    Type   *SignalType
    Kind   SignalKind  // Wire, Reg, Const
    Value  interface{} // For constants
    Source token.Pos
}

type SignalType struct {
    Width  int
    Signed bool
}

type SignalKind int
const (
    Wire SignalKind = iota
    Reg
    Const
)

type Process struct {
    Blocks      []*BasicBlock
    Sensitivity Sensitivity  // Combinational or Sequential
}

type Sensitivity int
const (
    Combinational Sensitivity = iota
    Sequential  // Driven by clock
)

type BasicBlock struct {
    Label string
    Ops   []Operation
}

type Operation interface {
    IsOperation()
}

// Concrete operation types
type BinOperation struct {
    Op     BinOp
    Dest   *Signal
    Left   *Signal
    Right  *Signal
}

type BinOp int
const (
    Add BinOp = iota
    Sub
    Mul
    And
    Or
    Xor
)

func (BinOperation) IsOperation() {}

type AssignOperation struct {
    Dest  *Signal
    Value *Signal
}

func (AssignOperation) IsOperation() {}
```

**Builder Logic:**

```go
// builder.go
package ir

import (
    "fmt"
    "golang.org/x/tools/go/ssa"
    "mygo/internal/diag"
)

type Builder struct {
    design   *Design
    reporter *diag.Reporter

    // Symbol table: SSA value → Signal
    signals  map[ssa.Value]*Signal
}

func BuildDesign(prog *ssa.Program, reporter *diag.Reporter) (*Design, error) {
    builder := &Builder{
        design:   &Design{Modules: make([]*Module, 0)},
        reporter: reporter,
        signals:  make(map[ssa.Value]*Signal),
    }

    // Find main package and main function
    mainPkg := prog.Package(prog.PackageFor(prog.ImportedPackage("main")))
    if mainPkg == nil {
        return nil, fmt.Errorf("no main package found")
    }

    mainFn := mainPkg.Func("main")
    if mainFn == nil {
        return nil, fmt.Errorf("no main function found")
    }

    // Translate main function to top-level module
    module, err := builder.buildModule(mainFn)
    if err != nil {
        return nil, err
    }

    builder.design.Modules = append(builder.design.Modules, module)
    builder.design.TopLevel = module

    return builder.design, nil
}

func (b *Builder) buildModule(fn *ssa.Function) (*Module, error) {
    module := &Module{
        Name:    fn.Name(),
        Ports:   []Port{},
        Signals: make(map[string]*Signal),
        Processes: make([]*Process, 0),
        Source:  fn.Pos(),
    }

    // Add default clock and reset ports
    module.Ports = append(module.Ports,
        Port{Name: "clk", Direction: Input, Type: &SignalType{Width: 1, Signed: false}},
        Port{Name: "rst", Direction: Input, Type: &SignalType{Width: 1, Signed: false}},
    )

    // Translate all basic blocks
    process := &Process{
        Blocks: make([]*BasicBlock, 0),
        Sensitivity: Sequential,
    }

    for _, block := range fn.Blocks {
        irBlock, err := b.buildBasicBlock(block, module)
        if err != nil {
            return nil, err
        }
        process.Blocks = append(process.Blocks, irBlock)
    }

    module.Processes = append(module.Processes, process)
    return module, nil
}

func (b *Builder) buildBasicBlock(block *ssa.BasicBlock, module *Module) (*BasicBlock, error) {
    irBlock := &BasicBlock{
        Label: fmt.Sprintf("bb%d", block.Index),
        Ops:   make([]Operation, 0),
    }

    for _, instr := range block.Instrs {
        switch v := instr.(type) {
        case *ssa.Alloc:
            sig := b.createSignal(v, module)
            sig.Kind = Reg

        case *ssa.Store:
            dest := b.getSignal(v.Addr, module)
            value := b.getSignal(v.Val, module)
            irBlock.Ops = append(irBlock.Ops, &AssignOperation{
                Dest:  dest,
                Value: value,
            })

        case *ssa.BinOp:
            dest := b.createSignal(v, module)
            left := b.getSignal(v.X, module)
            right := b.getSignal(v.Y, module)

            op, err := translateBinOp(v.Op)
            if err != nil {
                return nil, err
            }

            irBlock.Ops = append(irBlock.Ops, &BinOperation{
                Op:    op,
                Dest:  dest,
                Left:  left,
                Right: right,
            })

        case *ssa.UnOp:
            // Handle unary operations

        case *ssa.Return:
            // Handle return (output ports)

        case *ssa.Call:
            // Ignore fmt.Printf for now (or turn into debug signal)

        default:
            return nil, fmt.Errorf("unhandled instruction: %T", instr)
        }
    }

    return irBlock, nil
}

func (b *Builder) createSignal(v ssa.Value, module *Module) *Signal {
    if sig, exists := b.signals[v]; exists {
        return sig
    }

    sig := &Signal{
        Name:   v.Name(),
        Type:   inferSignalType(v.Type()),
        Kind:   Wire,
        Source: v.Pos(),
    }

    b.signals[v] = sig
    module.Signals[sig.Name] = sig
    return sig
}

func (b *Builder) getSignal(v ssa.Value, module *Module) *Signal {
    // Handle constants
    if c, ok := v.(*ssa.Const); ok {
        sig := &Signal{
            Name:  fmt.Sprintf("const_%v", c.Value),
            Type:  inferSignalType(c.Type()),
            Kind:  Const,
            Value: c.Value,
        }
        return sig
    }

    // Look up in symbol table
    if sig, exists := b.signals[v]; exists {
        return sig
    }

    // Create new signal
    return b.createSignal(v, module)
}

func inferSignalType(goType types.Type) *SignalType {
    basic, ok := goType.Underlying().(*types.Basic)
    if !ok {
        return &SignalType{Width: 32, Signed: false}  // Default
    }

    switch basic.Kind() {
    case types.Int8, types.Uint8:
        return &SignalType{Width: 8, Signed: basic.Info()&types.IsUnsigned == 0}
    case types.Int16, types.Uint16:
        return &SignalType{Width: 16, Signed: basic.Info()&types.IsUnsigned == 0}
    case types.Int32, types.Uint32:
        return &SignalType{Width: 32, Signed: basic.Info()&types.IsUnsigned == 0}
    case types.Int64, types.Uint64:
        return &SignalType{Width: 64, Signed: basic.Info()&types.IsUnsigned == 0}
    default:
        return &SignalType{Width: 32, Signed: false}
    }
}

func translateBinOp(op token.Token) (BinOp, error) {
    switch op {
    case token.ADD:
        return Add, nil
    case token.SUB:
        return Sub, nil
    case token.MUL:
        return Mul, nil
    case token.AND:
        return And, nil
    case token.OR:
        return Or, nil
    case token.XOR:
        return Xor, nil
    default:
        return 0, fmt.Errorf("unsupported binary op: %s", op)
    }
}
```

#### 4.2.6 Optimization Passes (`internal/passes`)

**Pass Interface:**

```go
// pass.go
package passes

import "mygo/internal/ir"

type Pass interface {
    Name() string
    Run(*ir.Design) error
}

type Manager struct {
    passes []Pass
}

func NewManager() *Manager {
    return &Manager{passes: make([]Pass, 0)}
}

func (m *Manager) Add(p Pass) {
    m.passes = append(m.passes, p)
}

func (m *Manager) Run(design *ir.Design) error {
    for _, pass := range m.passes {
        if err := pass.Run(design); err != nil {
            return fmt.Errorf("pass %s failed: %w", pass.Name(), err)
        }
    }
    return nil
}
```

**Constant Folding Example:**

```go
// constfold.go
package passes

import "mygo/internal/ir"

type ConstantFolder struct{}

func NewConstantFolder() Pass {
    return &ConstantFolder{}
}

func (cf *ConstantFolder) Name() string {
    return "constant-folding"
}

func (cf *ConstantFolder) Run(design *ir.Design) error {
    for _, module := range design.Modules {
        for _, process := range module.Processes {
            for _, block := range process.Blocks {
                cf.foldBlock(block)
            }
        }
    }
    return nil
}

func (cf *ConstantFolder) foldBlock(block *ir.BasicBlock) {
    newOps := make([]ir.Operation, 0)

    for _, op := range block.Ops {
        if binOp, ok := op.(*ir.BinOperation); ok {
            // Check if both operands are constants
            if binOp.Left.Kind == ir.Const && binOp.Right.Kind == ir.Const {
                // Compute constant result
                result := cf.evaluate(binOp)
                // Replace with assignment of constant
                newOps = append(newOps, &ir.AssignOperation{
                    Dest: binOp.Dest,
                    Value: &ir.Signal{
                        Kind:  ir.Const,
                        Type:  binOp.Dest.Type,
                        Value: result,
                    },
                })
                continue
            }
        }
        newOps = append(newOps, op)
    }

    block.Ops = newOps
}

func (cf *ConstantFolder) evaluate(op *ir.BinOperation) interface{} {
    // Simple integer constant folding
    left := op.Left.Value.(int64)
    right := op.Right.Value.(int64)

    switch op.Op {
    case ir.Add:
        return left + right
    case ir.Sub:
        return left - right
    case ir.Mul:
        return left * right
    default:
        return nil
    }
}
```

#### 4.2.7 MLIR Emission (`internal/mlir`)

**Emitter:**

```go
// emitter.go
package mlir

import (
    "fmt"
    "io"
    "os"
    "mygo/internal/ir"
    "mygo/internal/diag"
)

func EmitMLIR(design *ir.Design, outputPath string, reporter *diag.Reporter) error {
    var writer io.Writer

    if outputPath == "" || outputPath == "-" {
        writer = os.Stdout
    } else {
        f, err := os.Create(outputPath)
        if err != nil {
            return err
        }
        defer f.Close()
        writer = f
    }

    printer := NewPrinter(writer)

    // Emit module definitions
    for _, module := range design.Modules {
        if err := printer.EmitModule(module); err != nil {
            return err
        }
    }

    return nil
}

// printer.go
type Printer struct {
    w      io.Writer
    indent int
}

func NewPrinter(w io.Writer) *Printer {
    return &Printer{w: w, indent: 0}
}

func (p *Printer) EmitModule(module *ir.Module) error {
    // Build port list
    inputs := []string{}
    outputs := []string{}

    for _, port := range module.Ports {
        portDecl := fmt.Sprintf("%%%s: i%d", port.Name, port.Type.Width)
        if port.Direction == ir.Input {
            inputs = append(inputs, portDecl)
        } else {
            outputs = append(outputs, portDecl)
        }
    }

    // Emit module header
    fmt.Fprintf(p.w, "hw.module @%s(", module.Name)
    for i, in := range inputs {
        if i > 0 {
            fmt.Fprintf(p.w, ", ")
        }
        fmt.Fprintf(p.w, "%s", in)
    }
    fmt.Fprintf(p.w, ")")

    if len(outputs) > 0 {
        fmt.Fprintf(p.w, " -> (")
        for i, out := range outputs {
            if i > 0 {
                fmt.Fprintf(p.w, ", ")
            }
            fmt.Fprintf(p.w, "%s", out)
        }
        fmt.Fprintf(p.w, ")")
    }

    fmt.Fprintf(p.w, " {\n")
    p.indent++

    // Emit signal declarations and operations
    valueMap := make(map[*ir.Signal]string)
    nextSSA := 0

    for _, process := range module.Processes {
        for _, block := range process.Blocks {
            for _, op := range block.Ops {
                switch o := op.(type) {
                case *ir.BinOperation:
                    ssaName := fmt.Sprintf("%%v%d", nextSSA)
                    nextSSA++
                    valueMap[o.Dest] = ssaName

                    p.printIndent()
                    fmt.Fprintf(p.w, "%s = comb.%s %s, %s : i%d\n",
                        ssaName,
                        binOpName(o.Op),
                        p.getValueRef(o.Left, valueMap),
                        p.getValueRef(o.Right, valueMap),
                        o.Dest.Type.Width,
                    )

                case *ir.AssignOperation:
                    // For now, just track the assignment
                    valueMap[o.Dest] = p.getValueRef(o.Value, valueMap)
                }
            }
        }
    }

    // Emit output
    p.printIndent()
    fmt.Fprintf(p.w, "hw.output\n")

    p.indent--
    fmt.Fprintf(p.w, "}\n")

    return nil
}

func (p *Printer) getValueRef(sig *ir.Signal, valueMap map[*ir.Signal]string) string {
    if sig.Kind == ir.Const {
        return fmt.Sprintf("%%c%v", sig.Value)
    }

    if ref, exists := valueMap[sig]; exists {
        return ref
    }

    return "%" + sig.Name
}

func (p *Printer) printIndent() {
    for i := 0; i < p.indent; i++ {
        fmt.Fprintf(p.w, "  ")
    }
}

func binOpName(op ir.BinOp) string {
    switch op {
    case ir.Add:
        return "add"
    case ir.Sub:
        return "sub"
    case ir.Mul:
        return "mul"
    default:
        return "unknown"
    }
}
```

#### 4.2.8 Diagnostics (`internal/diag`)

```go
// reporter.go
package diag

import (
    "fmt"
    "go/token"
    "io"
)

type Severity int

const (
    Info Severity = iota
    Warning
    Error
    Fatal
)

type Diagnostic struct {
    Severity Severity
    Pos      token.Position
    Message  string
}

type Reporter struct {
    w       io.Writer
    format  string  // "text" or "json"
    fset    *token.FileSet
    errors  int
}

func NewReporter(w io.Writer, format string) *Reporter {
    return &Reporter{
        w:      w,
        format: format,
        fset:   token.NewFileSet(),
    }
}

func (r *Reporter) SetFileSet(fset *token.FileSet) {
    r.fset = fset
}

func (r *Reporter) Error(pos token.Pos, msg string) {
    r.report(Diagnostic{
        Severity: Error,
        Pos:      r.fset.Position(pos),
        Message:  msg,
    })
    r.errors++
}

func (r *Reporter) Warning(pos token.Pos, msg string) {
    r.report(Diagnostic{
        Severity: Warning,
        Pos:      r.fset.Position(pos),
        Message:  msg,
    })
}

func (r *Reporter) Fatal(err error) {
    fmt.Fprintf(r.w, "fatal error: %v\n", err)
    os.Exit(1)
}

func (r *Reporter) report(d Diagnostic) {
    if r.format == "json" {
        r.reportJSON(d)
    } else {
        r.reportText(d)
    }
}

func (r *Reporter) reportText(d Diagnostic) {
    severity := "error"
    switch d.Severity {
    case Info:
        severity = "info"
    case Warning:
        severity = "warning"
    }

    fmt.Fprintf(r.w, "%s:%d:%d: %s: %s\n",
        d.Pos.Filename,
        d.Pos.Line,
        d.Pos.Column,
        severity,
        d.Message,
    )
}

func (r *Reporter) reportJSON(d Diagnostic) {
    // Implement JSON output for editor integration
}

func (r *Reporter) HasErrors() bool {
    return r.errors > 0
}
```

---

## 5. Development Phases

### Phase 1: Minimal Viable Compiler (Weeks 1-4)

**Goal:** Successfully compile `simple.go` to MLIR

**Tasks:**

1. **Setup Project Structure** (Week 1)
 - [ ] Create directory layout
 - [ ] Initialize Go module (`go mod init mygo`)
 - [ ] Setup basic CLI with cobra/flag parsing
 - [ ] Add vendor directory for LLGo components

2. **Implement Frontend** (Week 2)
 - [ ] Implement `internal/frontend/loader.go`
 - [ ] Load `simple.go` using `go/packages`
 - [ ] Implement `internal/frontend/ssa.go`
 - [ ] Generate SSA and dump to stdout for inspection

3. **Implement IR Builder** (Week 3)
 - [ ] Define IR types in `internal/ir/`
 - [ ] Implement SSA → IR translation for:
 - Variable declarations (`*ssa.Alloc`)
 - Constants (`*ssa.Const`)
 - Binary operations (`*ssa.BinOp`)
 - Assignments (`*ssa.Store`)
 - [ ] Create symbol table mapping SSA values to signals

4. **Implement MLIR Emission** (Week 4)
 - [ ] Implement basic MLIR printer
 - [ ] Emit `hw.module` with clock/reset ports
 - [ ] Emit `comb.add` for addition operations
 - [ ] Test with `mlir-opt --verify-diagnostics`

**Success Criteria:**
```bash
$ mygo compile test/e2e/simple.go --emit=mlir -o simple.mlir
$ mlir-opt --verify-diagnostics simple.mlir
# Should pass without errors
```

**Expected Output (`simple.mlir`):**

```mlir
module {
  hw.module @main(%clk: i1, %rst: i1) {
    %c3 = hw.constant 3 : i32
    %c1 = hw.constant 1 : i32
    %c2 = hw.constant 2 : i16

    // dead = 3
    %dead = seq.compreg %c3, %clk : i32

    // l = dead
    %l = seq.compreg %dead, %clk : i32

    // i = 1
    %i = seq.compreg %c1, %clk : i32

    // j = 2
    %j = seq.compreg %c2, %clk : i16

    // k = i + j (with type extension)
    %j_ext = comb.zext %j : (i16) -> i64
    %i_ext = comb.sext %i : (i32) -> i64
    %k_val = comb.add %i_ext, %j_ext : i64
    %k = seq.compreg %k_val, %clk : i64

    hw.output
  }
}
```

### Phase 2: Type System & Validation (Weeks 5-6)

**Goal:** Proper type handling and error reporting

**Tasks:**

1. **Bit-width Inference**
 - [ ] Implement `internal/passes/widthinfer.go`
 - [ ] Automatic widening for mixed-type operations
 - [ ] Detect overflow conditions

2. **SSA Validation**
 - [ ] Implement `internal/validate/checker.go`
 - [ ] Whitelist allowed SSA instructions
 - [ ] Reject goroutines, channels, interfaces

3. **Enhanced Diagnostics**
 - [ ] Implement source position tracking
 - [ ] Pretty-print error messages with source snippets
 - [ ] Add JSON output mode

**Test Cases:**
```go
// test/e2e/type_mismatch.go
var a int8 = 100
var b int64 = 200
c := a + b  // Should auto-widen to int64
```

### Phase 3: Control Flow (Weeks 7-9)

**Goal:** Support `if` statements and bounded loops

**Tasks:**

1. **If Statement Translation**
 - [ ] Translate `*ssa.If` to multiplexers
 - [ ] Handle phi nodes for merged values

2. **Bounded Loops**
 - [ ] Detect compile-time loop bounds
 - [ ] Unroll or create state machine
 - [ ] Reject unbounded loops

**Example:**
```go
var sum int32 = 0
for i := 0; i < 10; i++ {
    sum = sum + i
}
```

### Phase 4: Verilog Backend (Weeks 10-12)

**Goal:** Generate synthesizable Verilog

**Tasks:**

1. **CIRCT Integration**
 - [ ] Invoke `circt-opt` with verilog export
 - [ ] Handle tool errors gracefully

2. **E2E Testing**
 - [ ] Golden file tests for Verilog output
 - [ ] Simulation with Verilator or Icarus

### Phase 5: Advanced Features (Weeks 13+)

- Arrays and memories
- Parameterization
- Pipeline annotations
- Optimization passes (dead code elimination, CSE)

---

## 6. Implementation Details

### 6.1 Partial Fork Strategy

**Why vendor LLGo components?**
- LLGo is actively developed; we want stability
- Only need subset (package loader, SSA builder)
- Avoid dependency hell

**Vendoring Process:**

```bash
# Initial vendor
mkdir -p third_party/llgo
cd third_party/llgo
git clone https://github.com/goplus/llgo.git tmp
cp -r tmp/internal/packages ./packages
rm -rf tmp

# Update mechanism (scripts/update-llgo.sh)
#!/bin/bash
cd third_party/llgo
git clone --depth=1 https://github.com/goplus/llgo.git tmp
cp -r tmp/internal/packages ./packages
rm -rf tmp
git diff  # Review changes
```

### 6.2 SSA Translation Patterns

**Pattern 1: Variable Declaration**

```go
// Go code
var x int32
```

```
// SSA
t0 = local int32 (x)
```

```mlir
// MLIR
%x = hw.wire : i32
```

**Pattern 2: Constant Assignment**

```go
// Go code
x = 42
```

```
// SSA
t0 = local int32 (x)
store 42 to t0
```

```mlir
// MLIR
%c42 = hw.constant 42 : i32
%x = seq.compreg %c42, %clk : i32
```

**Pattern 3: Binary Operation**

```go
// Go code
z = x + y
```

```
// SSA
t0 = local int32 (x)
t1 = local int32 (y)
t2 = load t0
t3 = load t1
t4 = add t2, t3
t5 = local int32 (z)
store t4 to t5
```

```mlir
// MLIR
%sum = comb.add %x, %y : i32
%z = seq.compreg %sum, %clk : i32
```

### 6.3 CIRCT Dialect Mapping

| Go Construct | SSA Instruction | CIRCT Operation |
|--------------|----------------|-----------------|
| `var x int32` | `*ssa.Alloc` | `hw.wire` or `seq.compreg` |
| `x = 5` | `*ssa.Store` | `hw.constant` + register |
| `z = x + y` | `*ssa.BinOp(ADD)` | `comb.add` |
| `z = x - y` | `*ssa.BinOp(SUB)` | `comb.sub` |
| `z = x * y` | `*ssa.BinOp(MUL)` | `comb.mul` |
| `z = x & y` | `*ssa.BinOp(AND)` | `comb.and` |
| `z = x \| y` | `*ssa.BinOp(OR)` | `comb.or` |
| `z = !x` | `*ssa.UnOp(NOT)` | `comb.xor` (with all-ones) |
| `if cond { }` | `*ssa.If` | `comb.mux` |

### 6.4 Determinism Requirements

**Why determinism matters:**
- Reproducible builds
- Reliable testing (golden files)
- Debuggability

**Strategies:**
1. **Sort map iterations:**
```go
   keys := make([]string, 0, len(signals))
   for k := range signals {
       keys = append(keys, k)
   }
   sort.Strings(keys)
   for _, k := range keys {
       // Process signals[k]
   }
   ```

2. **Canonical naming:**
   ```go
   // Use counter for SSA values
   nextSSA := 0
   name := fmt.Sprintf("%%v%d", nextSSA)
   nextSSA++
   ```

3. **Stable basic block ordering:**
   - Process blocks in index order
   - Don't rely on map iteration

---

## 7. Testing Strategy

### 7.1 Unit Tests

**Example: Test Signal Type Inference**

```go
// internal/ir/builder_test.go
package ir

import (
 "go/types"
 "testing"
)

func TestInferSignalType(t *testing.T) {
 tests := []struct {
 name string
 goType types.Type
 expected *SignalType
 }{
 {
 name: "int32",
 goType: types.Typ[types.Int32],
 expected: &SignalType{Width: 32, Signed: true},
 },
 {
 name: "uint16",
 goType: types.Typ[types.Uint16],
 expected: &SignalType{Width: 16, Signed: false},
 },
 }

 for _, tt := range tests {
 t.Run(tt.name, func(t *testing.T) {
 result := inferSignalType(tt.goType)
 if result.Width != tt.expected.Width {
 t.Errorf("width: got %d, want %d", result.Width, tt.expected.Width)
 }
 if result.Signed != tt.expected.Signed {
 t.Errorf("signed: got %v, want %v", result.Signed, tt.expected.Signed)
 }
 })
 }
}
```

### 7.2 End-to-End Tests

```go
// test/e2e/e2e_test.go
package e2e

import (
 "os"
 "os/exec"
 "path/filepath"
 "testing"
)

func TestSimpleCompilation(t *testing.T) {
 goFile := "simple.go"
 expectedMLIR := "simple.mlir"

 // Run compiler
 cmd := exec.Command("../../mygo", "compile", goFile, "--emit=mlir", "-o", "output.mlir")
 if err := cmd.Run(); err != nil {
 t.Fatalf("compilation failed: %v", err)
 }
 defer os.Remove("output.mlir")

 // Compare with golden file
 expected, err := os.ReadFile(expectedMLIR)
 if err != nil {
 t.Fatalf("read golden file: %v", err)
 }

 actual, err := os.ReadFile("output.mlir")
 if err != nil {
 t.Fatalf("read output: %v", err)
 }

 if !bytes.Equal(normalize(expected), normalize(actual)) {
 t.Errorf("output mismatch\nExpected:\n%s\nActual:\n%s", expected, actual)
 }

 // Verify with mlir-opt
 cmd = exec.Command("mlir-opt", "--verify-diagnostics", "output.mlir")
 if err := cmd.Run(); err != nil {
 t.Errorf("mlir-opt verification failed: %v", err)
 }
}

func normalize(data []byte) []byte {
 // Remove comments, normalize whitespace
 // Implementation details...
}
```

**Update Golden Files:**

```bash
# scripts/update-goldens.sh
#!/bin/bash
cd test/e2e
for f in *.go; do
 base="${f%.go}"
 ../../mygo compile "$f" --emit=mlir -o "$base.mlir"
done
```

### 7.3 CI Pipeline

```yaml
# .github/workflows/ci.yml
name: CI

on: [push, pull_request]

jobs:
test:
 runs-on: ubuntu-latest

 steps:
 - uses: actions/checkout@v3

 - name: Set up Go
 uses: actions/setup-go@v4
 with:
 go-version: '1.22'

 - name: Install CIRCT
 run: |
 ./scripts/install-circt.sh
 echo "$HOME/circt/bin" >> $GITHUB_PATH

 - name: Run unit tests
 run: go test ./internal/...

 - name: Lint
 uses: golangci/golangci-lint-action@v3
 with:
 version: latest

 - name: Build CLI
 run: go build -o mygo ./cmd/mygo

 - name: Run E2E tests
 run: go test ./test/e2e/...

 - name: Verify golden files
 run: |
 ./scripts/update-goldens.sh
 git diff --exit-code test/e2e/*.mlir
```

---

## 8. Coding Standards

### 8.1 Go Style

```go
// Good: Clear function documentation
// BuildModule translates an SSA function into a hardware Module.
// It creates a Module with clock/reset ports and translates all
// basic blocks into a sequential process.
//
// Returns an error if the function contains unsupported constructs.
func (b *Builder) BuildModule(fn *ssa.Function) (*Module, error) {
 // Implementation...
}

// Good: Error wrapping with context
if err := validateInstruction(instr); err != nil {
 return fmt.Errorf("validate %s at %s: %w",
 instr.Name(),
 fset.Position(instr.Pos()),
 err)
}

// Bad: Panic on user error
if unsupported {
 panic("unsupported construct") // DON'T DO THIS
}

// Good: Use reporter
if unsupported {
 reporter.Error(instr.Pos(), "unsupported construct: goroutine")
 return ErrUnsupported
}
```

### 8.2 Testing Discipline

```go
// Good: Table-driven tests
func TestTranslateBinOp(t *testing.T) {
 tests := []struct {
 name string
 goOp token.Token
 wantOp BinOp
 wantErr bool
 }{
 {"add", token.ADD, Add, false},
 {"sub", token.SUB, Sub, false},
 {"div", token.QUO, 0, true}, // Unsupported
 }

 for _, tt := range tests {
 t.Run(tt.name, func(t *testing.T) {
 got, err := translateBinOp(tt.goOp)
 if (err != nil) != tt.wantErr {
 t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
 }
 if got != tt.wantOp {
 t.Errorf("got %v, want %v", got, tt.wantOp)
 }
 })
 }
}
```

### 8.3 Documentation

**README.md Structure:**

```markdown
# MyGO: Go to Hardware Compiler

## Quick Start

## Installation

## Usage Examples

## Supported Syntax

## Architecture

## Development

## Contributing

## License
```

**Code Documentation:**
- Every exported symbol must have a doc comment
- Package-level doc in `doc.go`
- Complex algorithms need inline comments

---

## 9. References & Resources

### 9.1 Go Language

- [A Tour of Go](https://go.dev/tour/)
- [Effective Go](https://go.dev/doc/effective_go)
- [Go SSA Package](https://pkg.go.dev/golang.org/x/tools/go/ssa)
- [Go AST Package](https://pkg.go.dev/go/ast)

### 9.2 MLIR & CIRCT

- [MLIR Documentation](https://mlir.llvm.org/)
- [CIRCT Project](https://circt.llvm.org/)
- [CIRCT Dialects](https://circt.llvm.org/docs/Dialects/)
- [CIRCT Rationale](https://circt.llvm.org/docs/RationaleSimplifiedChinese/)

### 9.3 Hardware Design

- [Digital Design and Computer Architecture](https://www.amazon.com/Digital-Design-Computer-Architecture-Harris/dp/0123944244) (Textbook)
- [Verilog Tutorial](https://www.asic-world.com/verilog/veritut.html)

### 9.4 Similar Projects

- [argo2verilog](https://github.com/rmartin101/argo2verilog) - Inspiration for syntax
- [LLGo](https://github.com/goplus/llgo) - Frontend basis
- [Chisel](https://www.chisel-lang.org/) - Scala-based HDL
- [MyHDL](http://www.myhdl.org/) - Python-based HDL

### 9.5 Tools

- [Visual Studio Code](https://code.visualstudio.com/) with Go extension
- [Verilator](https://www.veripool.org/verilator/) - Verilog simulator
- [GTKWave](http://gtkwave.sourceforge.net/) - Waveform viewer

---

## Appendix A: Example Session

```bash
# Step 1: Write a simple Go program
cat > add.go <<EOF
package main

func main() {
 var a, b, sum int32
 a = 10
 b = 20
 sum = a + b
}
EOF

# Step 2: Compile to MLIR
mygo compile add.go --emit=mlir -o add.mlir

# Step 3: Inspect MLIR
cat add.mlir

# Step 4: Verify MLIR
mlir-opt --verify-diagnostics add.mlir

# Step 5: Generate Verilog
mygo compile add.go --emit=verilog -o add.sv

# Step 6: Inspect Verilog
cat add.sv

# Step 7: Simulate (optional)
verilator --lint-only add.sv
```

## Appendix B: Troubleshooting

**Problem:** `mlir-opt: command not found`

**Solution:**
```bash
export PATH=$HOME/circt/bin:$PATH
# Or reinstall CIRCT
./scripts/install-circt.sh
```

**Problem:** `cannot find package "golang.org/x/tools/go/ssa"`

**Solution:**
```bash
go mod tidy
go mod download
```

**Problem:** SSA dump shows `<nil>` for types

**Solution:**
Ensure you're loading packages with `NeedTypes | NeedTypesInfo` mode.

---

## Appendix C: First Milestone Checklist

For `simple.go` compilation:

- [ ] Project structure created
- [ ] CLI accepts `mygo compile simple.go`
- [ ] Frontend loads `simple.go` without errors
- [ ] SSA generated and can be dumped (`mygo dump-ssa simple.go`)
- [ ] IR builder translates variables
- [ ] IR builder translates constants
- [ ] IR builder translates assignments
- [ ] IR builder translates addition (`k = i + j`)
- [ ] MLIR emitter generates valid syntax
- [ ] `mlir-opt --verify-diagnostics output.mlir` passes
- [ ] Output matches expected structure

**Expected Timeline:** 4 weeks for a committed student working 10-15 hours/week.

---

**Document Version:** 0.1
**Last Updated:** 2025-11-02
**Maintainer:** Youwei Zhuo
