# Phase 3 Plan: Control Flow & Streaming Pipelines

Weeks **8-10** lift the prototype into a control-flow-aware compiler that can schedule goroutines/pipelines deterministically. This document expands on the outline in `README.md` §5.

---

## Objectives

1. **Structured control flow:** Translate SSA `if`, `phi`, and statically bounded `for` loops into IR blocks with explicit predicates/state.
2. **Pipeline scheduling:** Map goroutines/processes onto a deterministic multi-stage schedule that mirrors `argo_3stage` behavior (ready/valid, back-pressure).
3. **Channel semantics:** Enrich channels with handshake semantics (`ivalid/oready`), queue fullness tracking, and MLIR constructs that downstream CIRCT passes understand.
4. **Validation coverage:** Extend SSA validation to reject unsupported control-flow features (unbounded loops, `switch`, etc.) and ensure goroutine/channel creation stays static even after loop expansion.
5. **Testing:** Add end-to-end workloads (`pipeline1.go`, `pipeline2.go`, `router-csp.go`) plus control-flow focused unit tests and MLIR golden files.

---

## Work Breakdown

### 1. Control-Flow IR & Builder Updates
- Translate `*ssa.If`, `*ssa.Jump`, and `*ssa.Phi` into explicit `BasicBlock` successors with predicate wires.
- Model bounded loops as either unrolled sequences or state machines with loop-carried registers; introduce `LoopOperation`/`StateMachine` helpers as needed.
- Track dominance info so branch operands retain stable naming for later passes.

### 2. Validation Enhancements
- Enforce compile-time trip counts for `for` loops (`for i := 0; i < N; i++` style) and reject data-dependent exits.
- Disallow `switch`, `goto`, and `defer` until we implement the lowering story.
- Detect goroutines spawned from conditionals/loops that could create dynamic process counts unless guarded by compile-time conditions.

### 3. Scheduler / Process Metadata
- Introduce an IR-level schedule (e.g. `Process.ScheduleStage`) so goroutines can be assigned to explicit pipeline stages.
- Represent ready/valid handshake ports on channels; add fullness metadata for MLIR emission (`mygo.channel.ready`, `mygo.channel.valid` placeholders).
- Hook the scheduler into MLIR so each stage can emit `seq`/`comb` ops with clear cycle boundaries.

### 4. MLIR / CIRCT Dialect Prep
- Emit prototype `hw.instance` blocks for `argo_queue`/pipeline stage shells (even if they are stubs) to exercise CIRCT-friendly structure.
- Add custom ops or attributes to tag control-flow merges, preparing for future lowering to `cf` / `scf` / `seq`.
- Leave TODO hooks where Phase 4’s Verilog backend will attach real CIRCT transformations.

### 5. Tooling & Docs
- Update `README.md` and `README_PHASE3.md` with control-flow semantics, supported loop patterns, and new CLI flags (if any).
- Extend `mygo lint` with `--controlflow` mode to check for unsupported `if`/`for` patterns early.
- Document scheduling rules (number of stages, handshake expectations) with diagrams referencing `third_party/argo2verilog/src/verilog/argo_3stage.v`.

---

## Timeline & Milestones

| Week | Milestone | Exit Criteria |
|------|-----------|---------------|
| 8 | Control-flow IR prototype | `go test ./internal/ir -run Control` passes; `test/e2e/simple_branch` emits MLIR with `comb.mux`/state blocks. |
| 9 | Scheduler & handshake | Scheduler assigns goroutines to stages; `pipeline1.go` MLIR shows ready/valid plumbing; validation rejects unbounded loops. |
| 10 | Expanded e2e coverage | `pipeline1.go`, `pipeline2.go`, `router-csp.go` MLIR goldens checked in; docs updated with timing diagrams; CI runs new tests. |

---

## Test Plan

1. **Unit tests**
   - `internal/ir`: control-flow builder fixtures (`if`/loop lowering, phi resolution).
   - `internal/validate`: bounded loop validators, scheduler invariants.
   - `internal/mlir`: printer tests for new handshake ops or stage annotations.
2. **End-to-end tests**
   - Import Argo pipelines (`third_party/argo2verilog/test/pipeline1.go`, etc.) into `test/e2e/<case>`.
   - Capture MLIR goldens plus textual schedule summaries.
3. **Optional simulation hooks**
   - For each pipeline test, dump stage-by-stage token flow (even if just textual) to match Argo reference behavior.

---

## Deliverables

- Updated compiler components (`internal/ir`, `internal/mlir`, `internal/validate`, `internal/passes`) that understand control flow and stage scheduling.
- New linting modes + documentation describing supported control structures and pipeline rules.
- Golden MLIR outputs for the Argo pipeline benchmarks.
- CI updates to run the expanded unit/e2e suites.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Lowering SSA `phi` nodes incorrectly wires registers. | Add dedicated builder tests with explicit phi graphs; assert dominance relationships. |
| Scheduler deadlocks channels (missing ready/valid). | Model handshake signals in IR, add invariants/tests verifying every `Send` has a producer stage and consumer stage. |
| Loop validation too strict or too lax. | Implement precise trip-count analysis (literal bounds, `const` identifiers) and fallback diagnostics that explain how to restructure loops. |
| MLIR verbosity slows iteration. | Keep custom `mygo.*` ops lightweight with TODO comments; actual CIRCT lowering happens in Phase 4. |

---

## References

- `third_party/argo2verilog/test/pipeline1.go`, `pipeline2.go`, `router-csp.go` — ground truth control/pipeline behavior.
- `third_party/argo2verilog/src/verilog/argo_3stage.v` — target ready/valid scheduling.
- `README.md` §5.3 — high-level Phase 3 description. 
