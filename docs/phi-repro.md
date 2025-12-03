# Phi Lowering Reproduction

The `tests/stages/phi_loop` workload exercises a pair of counted loops
connected by a buffered channel. Each loop carries state via
`ir.PhiOperation`s, so this workload historically exposed the MLIR backend's
lack of phi lowering.

## Current Status (December 3, 2025)

Phi lowering now materializes explicit registers and predicate muxes
(`sv.reg` + `sv.passign`) in `internal/mlir/emitter.go`. Running the workload
through either the stage tests or the CLI succeeds without additional patches.
The snippet below is a quick sanity check that regenerates Verilog and compares
it to the checked-in golden:

```bash
PATH=$PWD/third_party/circt/build/bin:$PATH \
GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp \
go test ./tests/stages -run TestVerilogGeneration/phi_loop
```

You can also emit Verilog directly:

```bash
GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp \
go run ./cmd/mygo compile \
    -emit=verilog \
    --circt-opt=third_party/circt/build/bin/circt-opt \
    --fifo-src=internal/backend/templates/simple_fifo.sv \
    -o /tmp/phi_loop.sv \
    tests/stages/phi_loop/main.go
```

And the full simulator path mirrors the stage harness:

```bash
PATH=$PWD/third_party/circt/build/bin:$PATH \
GOCACHE=$PWD/.gocache GOTMPDIR=$PWD/.gotmp \
go run ./cmd/mygo sim \
    --sim-max-cycles 64 \
    --expect tests/stages/phi_loop/main.sim.golden \
    --fifo-src=internal/backend/templates/simple_fifo.sv \
    tests/stages/phi_loop/main.go
```

The `docs/phi-repro.md` file remains as a regression note; if either command
above fails, please update this document with the new failure signature.

## Historical Failure Mode

Before the lowering change, `circt-opt` rejected the intermediate MLIR because
the phi result never materialized and stayed as a dangling SSA reference:

```
/tmp/mygo-XXXX/design.mlir:42:24: error: expected SSA operand
    %v2 = comb.icmp ult, %t1_11, %c2 : i32
                       ^
backend: circt-opt --export-verilog failed: exit status 1
```
