# MyGO

MyGO is a research compiler that lowers subsets of Go into a structural MLIR/CIRCT representation and emits SystemVerilog for simulation. The toolchain bundles a CLI, IR passes, and a Verilog backend that can be wired to other simulators or the built-in Verilator harness.

## Requirements
- Go 1.22+ with GOPATH/bin on PATH.
- `circt-opt` (from CIRCT) and `verilator` available on PATH.

```bash
circt-opt --version
verilator --version
```

---

## Simple Workload
All commands below operate on `tests/stages/simple`, which runs without FIFOs and finishes in a couple of cycles.

```bash
# 1) Compile to MLIR (default emit)
go run ./cmd/mygo compile tests/stages/simple/main.go > /tmp/simple.mlir

# 2) Simulate with the built-in Verilator harness
go run ./cmd/mygo sim tests/stages/simple/main.go
```

### Extended Regression
```bash
# Pure Go unit+integration coverage
go test ./...

# Stage harness (MLIR, SV, Sim goldens)
go test ./tests/stages
```
These suites gracefully skip Verilog/Sim checks if `circt-opt` or `verilator` is missing, so it is safe to run them in CI and local shells.

---

## Docs
- `docs/compile.md` – full `mygo compile` flag reference, SSA/IR dump modes, lint workflow notes, FIFO guidance, and golden generation tips.
- `docs/sim.md` – simulator options, default Verilator flow, test structure, and how goldens/expectations work.
- `docs/phi-repro.md` – current known issues such as the `phi_loop` workload.
- `docs/backend/testdata.md` – catalog of backend SystemVerilog fixtures used in unit tests.
- `docs/archive/` – historical plans and previous READMEs for citation only.

Always update the relevant doc instead of bloating this README when you add flags or tweak flows.

---

## Repo Map
| Path | Purpose |
| ---- | ------- |
| `cmd/mygo` | CLI entry point (`compile`, `sim`, `lint`). `compile` now covers SSA/IR/MLIR/Verilog emission modes. |
| `internal/frontend`, `internal/ir`, `internal/mlir`, `internal/backend` | Compiler stages from Go loading to CIRCT emission. |
| `internal/backend/templates/simple_fifo.sv` | Reference FIFO implementation for channel-heavy workloads. |
| `tests/stages` | Golden-based stage harness (see `docs/sim.md`). |
| `scripts/` | Helper scripts such as `tidy.sh` for module hygiene. |

---

## Working for Contributors & Agents
- Run the **Workflow** commands before submitting or debugging a feature.
- When documenting or reviewing new CLI flags, place the explanation in `docs/compile.md` or `docs/sim.md` and link back here only if the quick path changes.
- Prefer `tests/stages/simple` for smoke coverage; introduce new workloads only when a behavior cannot be expressed there.
