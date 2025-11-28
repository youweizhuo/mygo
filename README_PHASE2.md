# Phase 2 Plan: Type System & Deterministic Concurrency

Weeks **5-7** focus on tightening the typed core while elevating goroutines and buffered channels to first-class features, mirroring the Argo programs under `third_party/argo2verilog`. This document augments the high-level roadmap in `README.md`.

---

## Status

✅ **Phase 2 complete (Week 7 exit criteria met).** Highlights:

- Width inference (`internal/passes/widthinfer.go`) propagates widths/signedness, inserts diagnostics for truncation, and runs by default inside the CLI.
- `validate.CheckProgram` plus the new `mygo lint --concurrency` command enforce deterministic goroutine/channel rules ahead of IR building.
- Goroutines become named IR processes with explicit channel metadata (`Send`/`Recv`/`Spawn` ops, FIFO depths, endpoint tracking) that the MLIR emitter surfaces via `mygo.channel.*` stubs.
- E2E samples were reorganized under `test/e2e/<case>/main.go`, including the new `channel_basic` workload that proves go/send/receive lowering.
- README docs now include concrete MLIR snippets and instructions for running both unit (`go test ./internal/...`) and scenario (`go test ./test/e2e`) suites.

To reproduce the final state:

```bash
go test ./...
go run ./cmd/mygo lint --concurrency test/e2e/channel_basic/main.go
go run ./cmd/mygo compile -emit=mlir -o channel_basic.mlir test/e2e/channel_basic/main.go
```

---

## Objectives

1. **Type soundness:** automatically infer widths/signedness, reject lossy mixes, and surface precise diagnostics.
2. **Deterministic concurrency:** translate `go` statements plus `make(chan T, N)` into explicit hardware processes and FIFOs, drawing on `third_party/argo2verilog/test/channel01.go`, `pipeline1.go`, and the `argo_queue` template in `third_party/argo2verilog/src/verilog`.
3. **Validation:** guarantee that only statically bounded goroutines/channels pass through SSA analysis, with clear error messages for unsupported constructs.
4. **Testing:** add end-to-end cases that prove MLIR/Verilog for the Argo channel workloads remains equivalent to the reference implementations.

---

## Work Breakdown

### 1. Width & Signedness Inference
- Implement `internal/passes/widthinfer.go` with lattice propagation over SSA values.
- Extend the IR `SignalType` with helpers for arithmetic promotion and saturation checks.
- Emit diagnostics when implicit truncation would occur; require explicit casts in such cases.

### 2. SSA Validation Updates
- Update `internal/validate/checker.go` with new rules:
  - Only allow `go` calls whose `Call.Value` is a named function.
  - Reject goroutines created inside loops unless the iteration count is one (prevents unbounded process creation).
  - Require each `make(chan)` call to use constant capacity and supported element types.
- Flag disallowed features early (e.g., `select`, maps, recursion) with actionable hints.

### 3. Channel & Process IR
- Introduce IR nodes for channels/FIFOs (width, depth, endpoint metadata) so downstream passes can stitch in `argo_queue` instances.
- Model goroutines as `ir.Process` entries with explicit port bindings (`go input(5, pipe1)` → process `@input` with `pipe1` output port).
- Carry handshake signals (`ivalid/oready`) so we can align with the control scheme in `third_party/argo2verilog/src/verilog/argo_3stage.v`.

### 4. MLIR Emission Enhancements
- Teach the printer/emitter to produce channel blocks, e.g., `hw.instance \"pipe1\" @fifo ...`.
- Emit `comb.mux`/`seq.compreg` structures for channel send/receive paths.
- Stub Verilog generation hooks that will later feed `circt-opt` (Phase 4), but ensure the MLIR already separates channel modules.

### 5. Diagnostics & Tooling
- Extend `diag.Reporter` with snippet rendering and JSON output needed by IDEs.
- Add targeted lint commands (`mygo lint --concurrency`) to catch banned patterns before full compilation.
- Document concurrency rules inline (Go doc + README edits already landed).

---

## Timeline & Milestones

| Week | Milestone | Exit Criteria |
|------|-----------|---------------|
| 5 | Width inference prototype | `go test ./internal/passes -run Width` passes; mixed-width arithmetic in `test/e2e/type_mismatch.go` compiles with inferred types. |
| 6 | Concurrency validation | `mygo lint` rejects dynamic goroutines/channels; unit tests cover `channel01.go` SSA snapshots. |
| 7 | Channel IR + MLIR | MLIR for `pipeline1.go` shows FIFO instances and goroutine processes; golden files checked in; README updated with timing diagram. |

---

## Test Plan

1. **Unit tests**
   - `internal/passes`: width inference lattice cases.
   - `internal/validate`: fixtures for allowed vs. rejected goroutines/channel creations.
2. **End-to-end tests**
   - Port `third_party/argo2verilog/test/channel01.go`, `pipeline1.go`, and `pipeline2.go` into `test/e2e`.
   - Compare MLIR via golden files; later phases will add Verilog/iverilog checks.
3. **Simulation parity (stretch)**
   - For each e2e test, dump signal traces and ensure ordering matches the reference printf output.

---

## Deliverables

- Updated compiler components (`internal/frontend`, `internal/ir`, `internal/passes`, `internal/validate`, `internal/mlir`) supporting concurrency-aware IR.
- Extended documentation (this file + spec updates) detailing the allowed goroutine/channel subset.
- Golden MLIR outputs for the Argo channel workloads.
- CI hooks that run the new unit/e2e suites.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Width inference oscillates or mis-infers signedness | Use a worklist algorithm with explicit top/bottom lattice values and add assertion-driven tests. |
| Channel deadlock due to missing back-pressure modeling | Mirror the `argo_3stage` control bits (ready/valid) in IR and unit-test FIFO fullness/emptiness propagation. |
| SSA validation misses dynamic goroutine creation patterns | Walk the AST as well as SSA to detect `go` statements nested in loops before SSA simplification hides them. |
| Verbose diagnostics slow iteration | Gate snippet rendering behind `--diag-format=pretty` and keep JSON output minimal. |

---

## References

- `third_party/argo2verilog/test/channel01.go`, `pipeline1.go`, `pipeline2.go` — ground truth for concurrency behavior.
- `third_party/argo2verilog/src/verilog/argo_queue.v`, `argo_3stage.v` — FIFO + pipeline templates we aim to reproduce.
- `README.md` §2.2 and §5 — high-level spec and roadmap that Phase 2 delivers on.
