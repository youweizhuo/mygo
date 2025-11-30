# MyGO

MyGO is a research compiler that lowers subsets of Go into a structural MLIR/CIRCT representation and emits SystemVerilog for simulation. The toolchain bundles a CLI, IR passes, and a Verilog backend that can be wired to other simulators or the built-in Verilator harness.

---

## Quick Start

```bash
# Clone and bootstrap
git clone https://github.com/.../mygo
cd mygo
go install ./cmd/mygo

# Verify prerequisites (Go 1.22+, CIRCT tools available on PATH)
circt-opt --version
verilator --version

# Smoke test
go test ./...
```

---

## CLI Usage

### Compile to MLIR
```bash
mygo compile -emit=mlir -o simple.mlir tests/e2e/simple/main.go
```

### Compile to Verilog
```bash
# Point --fifo-src at either a single SystemVerilog file or an entire directory of helper IP.
# The repo ships a sample at internal/backend/templates/simple_fifo.sv for quick validation.
mygo compile -emit=verilog \
    --circt-opt=$(which circt-opt) \
    --fifo-src=internal/backend/templates/simple_fifo.sv \
    -o simple.sv \
    tests/e2e/pipeline1/main.go
```

The backend removes auto-generated FIFO definitions from the main Verilog file and mirrors your FIFO/IP sources next to the output:

- `design_fifos.sv` when `--fifo-src` is a single file.
- `design_fifo_lib/<files>` when `--fifo-src` is a directory tree.

All auxiliary paths are reported via `backend.Result.AuxPaths` so you can hand them to downstream tools.

### Simulator Prints

`fmt.Print`, `fmt.Println`, and `fmt.Printf` calls whose first argument is a constant string literal lower directly to `$fwrite` statements inside `sv.always` blocks, so they fire once per module clock tick. The current formatter understands `%d`, `%b`, `%x`, `%t`, and `%%` and requires every substituted argument to be an integer or boolean SSA value. Unsupported verbs, non-literal format strings, or non-integer operands trigger a warning and the print is dropped so other programs keep compiling.

### Simulate
```bash
# Default run with the built-in Verilator harness (requires verilator on PATH)
mygo sim \
    --circt-opt=$(which circt-opt) \
    --fifo-src=internal/backend/templates/simple_fifo.sv \
    --sim-max-cycles=64 \
    tests/e2e/pipeline1/main.go

# Point to a custom simulator wrapper if needed
mygo sim \
    --circt-opt=$(which circt-opt) \
    --fifo-src=/path/to/my_fifo_lib \
    --simulator=/path/to/custom-sim.sh \
    --sim-args="--extra --flags" \
    tests/e2e/pipeline1/main.go

`--sim-max-cycles` and `--sim-reset-cycles` control how long the built-in driver
ticks the design before aborting, and `--expect <path>` enables golden trace
checks against the simulator stdout. When `--simulator` is omitted, Verilator is
invoked directly; otherwise, the CLI forwards all generated sources to the
custom wrapper. The default flow synthesizes a tiny C++ driver (`sim_main.cpp`)
plus a Verilator Makefile inside a temporary directory (e.g. `/tmp/mygo-verilator-*`)
before deleting it; add `--keep-artifacts` (optionally `--verilog-out`) if you
want to inspect the generated C++/Makefile/Verilog bundle.
```

`mygo sim` auto-detects `expected.sim` living next to a single Go input and fails fast if the simulator output differs.

---

## Key Modules

| Path | Description |
|------|-------------|
| `cmd/mygo` | Multi-command CLI (`compile`, `sim`, `dump-*`, `lint`). Hosts the simulation harness and flag plumbing for CIRCT binaries, FIFO sources, and expected traces. |
| `internal/frontend` | Loads Go sources via go/packages/SSA and produces the high-level IR. |
| `internal/ir` | Defines the hardware IR, processes, channels, and validation helpers. |
| `internal/mlir` | Lowers the IR to structural MLIR (`hw`, `seq`, `sv` dialects) and emits FIFO extern declarations. |
| `internal/backend` | Manages CIRCT temp files, optional `circt-opt` passes, Verilog emission, FIFO stripping, and mirroring of user-provided helper IP. |
| `tests/e2e` | End-to-end workloads (ported from Argo) plus golden MLIR and simulation traces. |
| `internal/backend/templates/simple_fifo.sv` | Reference FIFO implementation for quick experimentation. Copy/modify this outside the repo for production flows. |

---

## Testing

```bash
# Unit + integration suites
go test ./...

# Focus on backend/package tests
go test ./internal/backend -run .

# Run the e2e harness (compares MLIR, SV, and sim goldens)
go test ./tests/e2e -run TestProgramsLoweringAndSimulation
```

The CLI itself has regression coverage in `cmd/mygo/sim_test.go`, which stubs the CIRCT binaries and executes the built-in Verilator flow (or any custom simulator you point it at).

---

## Documentation & Archive

- Historical READMEs (Phase 1–4 plans and the previous monolithic README) now live in `docs/arxiv/mygo_archive.md` for reference or citation in arXiv write-ups.
- Templates, helper IP, and additional notes sit under `internal/backend/templates/` and `docs/`.

## Known Issues

- `tests/e2e/phi_loop` is a minimized workload that still triggers the current lack of phi lowering in the MLIR backend. Running the Verilog emission command documented in `docs/phi-repro.md` reproduces the failure until phis are lowered to concrete SSA values.

For architectural or research deep dives, start with the archived document above; keep this README handy for daily work and onboarding.
