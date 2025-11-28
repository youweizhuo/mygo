# Phase 4 Plan: Verilog Backend & Simulation

Weeks **11-13** bring the MyGO prototype from MLIR-only outputs to a Verilog-capable toolchain validated against the Argo reference designs. This phase layers a Verilog emission path on top of the MLIR we already generate, adds simulation hooks, and tightens the golden test matrix.

---

## Objectives

1. **CIRCT/Verilog backend:** Follow the Option 3 structural path: emit pure CIRCT `hw`/`seq`/`sv` dialects (one `hw.module` per Go function), replace `mygo.process.spawn` with `hw.instance` ops, route channel traffic through FIFO instances, run the necessary `circt-opt` pipelines, and export synthesizable SystemVerilog.
2. **FIFO/IP integration:** Materialize `argo_queue`-style FIFOs and any other helper IP (reset synchronizers, ready/valid shims) as reusable modules and instantiate them from the MLIR/Verilog backend.
3. **Simulation + verification:** Stand up a simulation harness (Icarus or Verilator) that can run the Argo workloads end-to-end and compare the textual outputs/prints to known-good traces.
4. **Automation & docs:** Expose the new functionality through the CLI (`mygo compile -emit=verilog`, `mygo sim`), document the workflow, and hook the Verilog/Sim steps into CI.

### Lowering strategy (Option 3 now, Option 2 later)

- **Current choice:** stay entirely in structural `hw`/`sv`. Each Go function becomes its own `hw.module`, goroutine launches are just `hw.instance` ops, and channels bind to FIFO modules with ready/valid style ports. That eliminates the ad hoc `mygo.process.*`/`mygo.channel.*` ops so `circt-translate --export-verilog` can run today.
- **Future-flex seam:** keep the backend entry point abstract (`mygo/internal/backend`) so we can swap in a Handshake-based lowering (Option 2) later without changing any CLI flags or CIRCT plumbing—only the MLIR handed to the backend changes.

---

## Work Breakdown

### 1. MLIR → Verilog Backend
- Introduce a `mygo/internal/backend` package that wraps `circt-opt`/`circt-translate` invocations and hides temp-file management.
- Expose `--circt-opt`, `--circt-translate`, `--circt-pipeline`, and `--circt-mlir` flags in `cmd/mygo` so local CIRCT installs and intermediate dumps are configurable.
- Update the MLIR emitter to produce the Option 3 structural form only (`hw.module`/`hw.instance`/`seq.compreg` + FIFO module declarations).
- Document how to point the backend at an alternate lowering (Hand-shake) without changing the CLI surface.

### 2. FIFO & IP Library
- Translate the existing channel metadata into concrete FIFO instantiations (`argo_queue` equivalent) with depth/width parameters. Instead of embedding the implementation, require users to pass `--fifo-src` so known-good IP (kept outside this repo) can be copied alongside the generated Verilog.
- Provide stubs for other shared blocks (e.g., balanced routers, ready/valid shims) and ensure they can either be emitted inline or referenced as external SV sources.
- Add configuration knobs for FIFO implementation style (simple regs vs. vendor RAM) to prepare for future phases.

### 3. Simulation Harness
- Add `mygo sim <case>` (or extend `mygo test`) to compile emitted Verilog with Icarus/Verilator and run the resulting executable.
- Provide a Verilator wrapper script (or documented command line) that the CLI/sim users can invoke locally.
- Capture standard output from the simulation and diff it against the reference traces from `third_party/argo2verilog` (starting with the new `test/e2e/pipeline1/expected.sim` golden).
- Extend `test/e2e` so each workload can optionally specify a `.sim.out` golden that the CI sim step compares against.

### 4. CLI & Documentation Updates
- Update `README.md` + `README_PHASE4.md` with Verilog/simulation instructions, environment requirements (CIRCT build, simulator install), and troubleshooting steps. Provide a worked example using `mygo sim --simulator <wrapper> --fifo-src <fifo> --expect <trace> test/e2e/pipeline1/main.go`.
- Document new CLI flags (`--circt-opt`, `--circt-translate`, `--circt-pipeline`, `--circt-mlir`, `--fifo-src`, `--simulator`, `--sim-args`, `--expect`) and add quickstart guides for both MLIR and Verilog flows.
- Keep a running list of supported Argo workloads and their golden artifacts (MLIR, SV, simulation logs).

---

## Timeline & Milestones

| Week | Milestone | Exit Criteria |
|------|-----------|---------------|
| 11 | CIRCT backend MVP | Option 3 lowering is live (`hw.instance` + FIFO modules) and `mygo compile -emit=verilog` drives CIRCT via the new backend flags; Verilog files for `pipeline1.go` pass `verilator --lint-only`. |
| 12 | FIFO/IP integration | Channels instantiate concrete FIFO modules with correct depth/width; generated SV links against the helper IP library without manual edits. |
| 13 | Simulation & CI | `mygo sim test/e2e/pipeline1` runs Verilator/Icarus and matches the Argo printf trace; CI runs MLIR, SV lint, and sim on all e2e workloads. |

---

## Test Plan

1. **Unit tests**
   - `internal/backend`: mock CIRCT invocations to ensure command-lines are formed correctly and diagnostics surface cleanly.
   - FIFO/IP generators: table-driven tests verifying port widths, stage assignments, and reset semantics.
2. **Integration tests**
   - Extend `test/e2e` to capture both MLIR and SV goldens (textual diff).
   - For at least one small design (`pipeline1.go`), run Verilator/Icarus in CI and compare stdout to a reference log.
3. **Manual sanity checks**
   - Run `circt-opt` + `circt-translate` manually on a few workloads when tweaking backend pipelines.
   - Spot-check waveform dumps (via GTKWave) to ensure handshake signals align with the ready/valid contract.

---

## Deliverables

- `mygo compile -emit=verilog` producing standalone `.sv` bundles (top module + helper FIFOs).
- `mygo sim` (or equivalent) that drives Icarus/Verilator and reports pass/fail based on textual traces.
- Updated documentation covering the Verilog flow, environment setup, and CI expectations.
- CI jobs that lint and (optionally) simulate the Argo workloads alongside the existing MLIR golden comparisons.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| CIRCT pipeline drift | Pin a known-good CIRCT commit in docs and provide a helper script to build/download that version. |
| FIFO timing issues | Start with simple behavioral FIFOs, add assertions, and gate more aggressive implementations behind flags. |
| Simulation flakiness | Capture deterministic printf traces; add retry logic and clear diagnostics when simulators are missing. |
| CI runtime | Gate full simulation to nightly runs while keeping lint/static checks on every PR. |
