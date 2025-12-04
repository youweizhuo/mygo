# `mygo sim`

Simulation stitches CIRCT-generated Verilog together with Verilator (default) or any custom simulator you provide. This document captures everything beyond the single-step README tutorial so you can debug goldens, extend the harness, or plug in different backends.

## Fast Path (Simple Workload)

```bash
mygo sim tests/stages/simple/main.go
```

- Requires `circt-opt` and `verilator` on `PATH`.
- The `simple` workload runs in one cycle and has no channels, so you can omit `--fifo-src`.
- The harness auto-detects `tests/stages/simple/expected.sim` and treats it as the golden trace.

## Golden + Test Structure

Workloads live in `tests/stages/<case>/` with the following optional files:

| File | Purpose |
| ---- | ------- |
| `main.go` | Go source under test (always present). |
| `main.mlir.golden` | Reference MLIR for `compile -emit=mlir`. |
| `main.sv.golden` | Reference SystemVerilog for `compile -emit=verilog`. |
| `main.sim.golden` | Reference simulator stdout for `sim`. |


`tests/stages/stages_test.go` wires these assets into three suites plus targeted regressions:

1. `TestSimulation` runs `go run ./cmd/mygo sim ...` with per-workload `--sim-max-cycles` and `--fifo-src` flags.
2. `TestSimulationDetectsMismatch` verifies `--expect` handling by pointing at a bad golden.
3. `TestSimulationVerilogOutWritesArtifacts` ensures `--verilog-out` mirrors the Verilog bundle when requested.

Keep the goldens checked in—`go test ./tests/stages` will skip simulator-dependent cases if `circt-opt` or `verilator` is missing, so CI can still pass on a pure Go toolchain.

## Default Verilator Harness

When `--simulator` is omitted, MyGO:

1. Emits Verilog + aux FIFO/IP files into a temp dir rooted alongside your workload (or `--verilog-out`).
2. Renders `sim_main.cpp` with `--sim-max-cycles` and `--sim-reset-cycles` baked in.
3. Invokes `verilator --cc --exe --build` with the generated bundle.
4. Runs the produced `mygo_sim` binary and optionally checks stdout against `--expect` / auto goldens.

Artifacts now stick around by default, so you can inspect them under `<workload>/mygo-sim-*`. Pass `--keep-artifacts=false` to opt back into auto-cleanup.

## Flag Reference

| Flag | Purpose |
| ---- | ------- |
| `-diag-format` | Diagnostic reporter format (`text` or `json`). |
| `--circt-opt` / `--circt-pipeline` / `--circt-lowering-options` / `--circt-mlir` | Same semantics as the compile command but applied before simulation. |
| `--verilog-out` | Path to write the Verilog bundle instead of a temp dir. Creates parent directories as needed. |
| `--keep-artifacts` | Preserve the temp dir containing Verilog, Makefile, and simulator outputs (default `true`). |
| `--simulator` | Custom executable to run instead of the built-in Verilator flow. Receives the main Verilog file plus aux files. |
| `--sim-args` | Extra arguments (split by spaces) forwarded to the custom simulator. |
| `--fifo-src` | Required when the design contains channels; accepts a file or directory similar to the compile command. |
| `--sim-max-cycles` | Max cycles for the built-in driver before declaring a timeout (default 16). Must be > 0. |
| `--sim-reset-cycles` | Number of cycles to hold reset high at startup (default 2). |
| `--expect` | Path to a golden stdout trace. |

## Workflow Notes for Contributors

- **Matching goldens:** Use `--expect tests/stages/<case>/main.sim.golden` during repro steps so failing diffs show up immediately. Update the golden file only after confirming the new behavior.
- **FIFO libraries:** Workloads marked `NeedsFIFO` in `stages_test.go` pass `--fifo-src internal/backend/templates/simple_fifo.sv`. The backend recognizes the `// mygo:fifo_template` marker inside this file and automatically appends per-channel wrapper modules (e.g. `mygo_fifo_i32_d1`) next to the emitted design, so the simulator sees concrete module names without any manual editing.
- **Custom simulator wrappers:** Provide `--simulator=/path/to/wrapper` plus any `--sim-args`. MyGO passes the generated Verilog as positional arguments so wrappers can re-run Verilator, hook into commercial tools, etc.
- **CI expectations:** `go test ./tests/stages` is the canonical way to exercise sim regressions. The suite enforces `circt-opt` + `verilator` availability before running expensive tests, so agents can safely call it even on machines without the full stack.
