package ir

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/ssa"

	"mygo/internal/diag"
)

// BuildDesign converts the SSA program into the hardware IR described in README.
func BuildDesign(prog *ssa.Program, reporter *diag.Reporter) (*Design, error) {
	mainPkg := findMainPackage(prog)
	if mainPkg == nil {
		return nil, fmt.Errorf("no main package found")
	}

	mainFn := mainPkg.Func("main")
	if mainFn == nil {
		return nil, fmt.Errorf("main function not found in package %s", mainPkg.Pkg.Path())
	}

	builder := &builder{
		reporter:      reporter,
		signals:       make(map[ssa.Value]*Signal),
		processes:     make(map[*ssa.Function]*Process),
		channels:      make(map[ssa.Value]*Channel),
		paramSignals:  make(map[*ssa.Parameter]*Signal),
		paramChannels: make(map[*ssa.Parameter]*Channel),
		channelUsage:  make(map[*Channel]int),
		nextStage:     1,
	}

	module := builder.buildModule(mainFn)
	if reporter.HasErrors() {
		return nil, fmt.Errorf("failed to build module")
	}

	design := &Design{
		Modules:  []*Module{module},
		TopLevel: module,
	}

	return design, nil
}

type builder struct {
	reporter      *diag.Reporter
	module        *Module
	signals       map[ssa.Value]*Signal
	processes     map[*ssa.Function]*Process
	channels      map[ssa.Value]*Channel
	paramSignals  map[*ssa.Parameter]*Signal
	paramChannels map[*ssa.Parameter]*Channel
	channelUsage  map[*Channel]int
	nextStage     int
	blocks        map[*ssa.BasicBlock]*BasicBlock
	tempID        int
}

func (b *builder) buildModule(fn *ssa.Function) *Module {
	mod := &Module{
		Name:     fn.Name(),
		Ports:    defaultPorts(),
		Signals:  make(map[string]*Signal),
		Channels: make(map[string]*Channel),
		Source:   fn.Pos(),
	}
	b.module = mod
	entry := b.buildProcess(fn)
	if entry != nil && entry.Stage < 0 {
		entry.Stage = 0
	}
	b.finalizeProcessStages()
	b.finalizeChannelOccupancy()

	return mod
}

func (b *builder) buildProcess(fn *ssa.Function) *Process {
	if proc, ok := b.processes[fn]; ok {
		return proc
	}
	proc := &Process{
		Name:        fn.Name(),
		Sensitivity: Sequential,
		Stage:       -1,
	}
	b.processes[fn] = proc
	b.module.Processes = append(b.module.Processes, proc)
	b.bindFunctionParams(fn)

	prevBlocks := b.blocks
	b.blocks = make(map[*ssa.BasicBlock]*BasicBlock)
	defer func() { b.blocks = prevBlocks }()

	ordered := make([]*ssa.BasicBlock, 0, len(fn.Blocks))
	for _, block := range fn.Blocks {
		if block == nil {
			continue
		}
		bb := &BasicBlock{Label: blockComment(block)}
		b.blocks[block] = bb
		proc.Blocks = append(proc.Blocks, bb)
		ordered = append(ordered, block)
	}

	for _, block := range ordered {
		b.translateBlock(proc, block)
	}
	b.connectBlocks(ordered)
	b.orderBlocks(proc)
	return proc
}

func (b *builder) translateBlock(proc *Process, block *ssa.BasicBlock) {
	if block == nil {
		return
	}
	bb := b.blocks[block]
	if bb == nil {
		return
	}
	for _, instr := range block.Instrs {
		switch v := instr.(type) {
		case *ssa.Phi:
			b.handlePhi(block, bb, v)
		case *ssa.If:
			b.handleIf(block, bb, v)
		case *ssa.Jump:
			b.handleJump(block, bb)
		case *ssa.Return:
			b.handleReturn(bb)
		default:
			b.translateInstr(proc, bb, instr)
		}
	}
}

func (b *builder) connectBlocks(blocks []*ssa.BasicBlock) {
	for _, block := range blocks {
		if block == nil {
			continue
		}
		src := b.blocks[block]
		if src == nil {
			continue
		}
		for _, succ := range block.Succs {
			if succ == nil {
				continue
			}
			dst := b.blocks[succ]
			if dst == nil {
				continue
			}
			src.Successors = append(src.Successors, dst)
			dst.Predecessors = append(dst.Predecessors, src)
		}
	}
}

func (b *builder) finalizeProcessStages() {
	if b.module == nil {
		return
	}
	for _, proc := range b.module.Processes {
		if proc == nil {
			continue
		}
		if proc.Stage < 0 {
			proc.Stage = 0
		}
		if proc.Stage >= b.nextStage {
			b.nextStage = proc.Stage + 1
		}
	}
}

func (b *builder) finalizeChannelOccupancy() {
	if b.module == nil {
		return
	}
	for _, ch := range b.module.Channels {
		if ch == nil {
			continue
		}
		occ := b.channelUsage[ch]
		if occ < 0 {
			occ = 0
		}
		if ch.Depth > 0 && occ > ch.Depth {
			occ = ch.Depth
		}
		ch.Occupancy = occ
	}
}

func (b *builder) ensureProcessStage(proc *Process) int {
	if proc == nil {
		return 0
	}
	if proc.Stage < 0 {
		proc.Stage = 0
	}
	return proc.Stage
}

func (b *builder) assignChildStage(parent, child *Process) {
	if child == nil {
		return
	}
	parentStage := b.ensureProcessStage(parent)
	desired := parentStage + 1
	if desired < b.nextStage {
		desired = b.nextStage
	}
	if child.Stage < desired {
		child.Stage = desired
	}
	if desired >= b.nextStage {
		b.nextStage = desired + 1
	}
}

func (b *builder) recordChannelDelta(ch *Channel, delta int) {
	if ch == nil {
		return
	}
	value := b.channelUsage[ch]
	value += delta
	if value < 0 {
		value = 0
	}
	b.channelUsage[ch] = value
}

func (b *builder) orderBlocks(proc *Process) {
	if proc == nil || len(proc.Blocks) == 0 {
		return
	}
	visited := make(map[*BasicBlock]bool)
	order := make([]*BasicBlock, 0, len(proc.Blocks))
	var visit func(*BasicBlock)
	visit = func(bb *BasicBlock) {
		if bb == nil || visited[bb] {
			return
		}
		visited[bb] = true
		for _, succ := range bb.Successors {
			visit(succ)
		}
		order = append(order, bb)
	}
	visit(proc.Blocks[0])
	for _, bb := range proc.Blocks {
		if !visited[bb] {
			visit(bb)
		}
	}
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}
	proc.Blocks = order
}

func (b *builder) handleIf(block *ssa.BasicBlock, bb *BasicBlock, stmt *ssa.If) {
	if bb == nil || stmt == nil {
		return
	}
	cond := b.signalForValue(stmt.Cond)
	if cond == nil {
		b.reporter.Warning(stmt.Pos(), "if condition has no signal mapping; treating as false")
	}
	var trueBB, falseBB *BasicBlock
	if len(block.Succs) > 0 {
		trueBB = b.blocks[block.Succs[0]]
	}
	if len(block.Succs) > 1 {
		falseBB = b.blocks[block.Succs[1]]
	}
	bb.Terminator = &BranchTerminator{
		Cond:  cond,
		True:  trueBB,
		False: falseBB,
	}
}

func (b *builder) handleJump(block *ssa.BasicBlock, bb *BasicBlock) {
	if bb == nil || block == nil {
		return
	}
	var target *BasicBlock
	if len(block.Succs) > 0 {
		target = b.blocks[block.Succs[0]]
	}
	bb.Terminator = &JumpTerminator{Target: target}
}

func (b *builder) handleReturn(bb *BasicBlock) {
	if bb == nil {
		return
	}
	bb.Terminator = &ReturnTerminator{}
}

func (b *builder) handlePhi(block *ssa.BasicBlock, bb *BasicBlock, phi *ssa.Phi) {
	if bb == nil || phi == nil {
		return
	}
	dest := b.ensureValueSignal(phi)
	dest.Type = signalType(phi.Type())
	incomings := make([]PhiIncoming, 0, len(phi.Edges))
	for idx, edge := range phi.Edges {
		var pred *BasicBlock
		if block != nil && idx < len(block.Preds) {
			pred = b.blocks[block.Preds[idx]]
		}
		value := b.signalForValue(edge)
		incomings = append(incomings, PhiIncoming{
			Block: pred,
			Value: value,
		})
	}
	b.signals[phi] = dest
	if mux := b.tryLowerPhiToMux(block, incomings, dest); mux != nil {
		bb.Ops = append(bb.Ops, mux)
		return
	}
	bb.Ops = append(bb.Ops, &PhiOperation{
		Dest:      dest,
		Incomings: incomings,
	})
}

func (b *builder) handleBinOp(bb *BasicBlock, op *ssa.BinOp) {
	if bb == nil || op == nil {
		return
	}
	left := b.signalForValue(op.X)
	right := b.signalForValue(op.Y)
	if left == nil || right == nil {
		return
	}
	if pred, ok := translateCompareOp(op.Op, isSignedType(op.X.Type())); ok {
		dest := b.ensureValueSignal(op)
		dest.Type = signalType(op.Type())
		bb.Ops = append(bb.Ops, &CompareOperation{
			Predicate: pred,
			Dest:      dest,
			Left:      left,
			Right:     right,
		})
		return
	}
	bin, ok := translateBinOp(op.Op)
	if ok && bin == ShrU && op.Op == token.SHR && isSignedType(op.X.Type()) {
		bin = ShrS
	}
	if !ok {
		b.reporter.Warning(op.Pos(), fmt.Sprintf("unsupported binary op: %s", op.Op.String()))
		return
	}
	dest := b.ensureValueSignal(op)
	dest.Type = signalType(op.Type())
	if isShiftBinOp(bin) {
		leftType := signalType(op.X.Type())
		if leftType != nil && (left.Type == nil || !left.Type.Equal(leftType)) {
			left.Type = leftType
		}
		if left.Type != nil && (right.Type == nil || !right.Type.Equal(left.Type)) {
			cast := b.newAnonymousSignal("shift", left.Type, op.Pos())
			bb.Ops = append(bb.Ops, &ConvertOperation{
				Dest:  cast,
				Value: right,
			})
			right = cast
		}
	}
	bb.Ops = append(bb.Ops, &BinOperation{
		Op:    bin,
		Dest:  dest,
		Left:  left,
		Right: right,
	})
}

func (b *builder) handleUnOp(proc *Process, bb *BasicBlock, op *ssa.UnOp) {
	if op == nil {
		return
	}
	switch op.Op {
	case token.MUL:
		ptr := b.signalForValue(op.X)
		if ptr != nil {
			b.signals[op] = ptr
		}
	case token.ARROW:
		b.handleRecv(proc, bb, op)
	case token.NOT:
		value := b.signalForValue(op.X)
		if value == nil {
			return
		}
		dest := b.ensureValueSignal(op)
		dest.Type = signalType(op.Type())
		bb.Ops = append(bb.Ops, &NotOperation{
			Dest:  dest,
			Value: value,
		})
	default:
		// TODO: support unary negation and bitwise complement as needed.
	}
}

func (b *builder) tryLowerPhiToMux(block *ssa.BasicBlock, incomings []PhiIncoming, dest *Signal) *MuxOperation {
	if block == nil || len(block.Preds) != 2 || len(incomings) != 2 {
		return nil
	}
	predA := block.Preds[0]
	predB := block.Preds[1]
	if predA == nil || predB == nil {
		return nil
	}
	if len(predA.Preds) == 0 || len(predB.Preds) == 0 {
		return nil
	}
	header := predA.Preds[0]
	if header == nil || header != predB.Preds[0] {
		return nil
	}
	if len(header.Succs) < 2 || len(header.Instrs) == 0 {
		return nil
	}
	ifInstr, ok := header.Instrs[len(header.Instrs)-1].(*ssa.If)
	if !ok {
		return nil
	}
	cond := b.signalForValue(ifInstr.Cond)
	if cond == nil {
		return nil
	}
	trueSucc := header.Succs[0]
	falseSucc := header.Succs[1]
	var trueVal, falseVal *Signal
	switch {
	case trueSucc == predA && falseSucc == predB:
		trueVal = incomings[0].Value
		falseVal = incomings[1].Value
	case trueSucc == predB && falseSucc == predA:
		trueVal = incomings[1].Value
		falseVal = incomings[0].Value
	default:
		return nil
	}
	if trueVal == nil || falseVal == nil {
		return nil
	}
	return &MuxOperation{
		Dest:       dest,
		Cond:       cond,
		TrueValue:  trueVal,
		FalseValue: falseVal,
	}
}

func (b *builder) translateInstr(proc *Process, bb *BasicBlock, instr ssa.Instruction) {
	switch v := instr.(type) {
	case *ssa.Alloc:
		b.handleAlloc(v)
	case *ssa.Store:
		dest := b.signalForValue(v.Addr)
		val := b.signalForValue(v.Val)
		if dest == nil || val == nil {
			return
		}
		bb.Ops = append(bb.Ops, &AssignOperation{Dest: dest, Value: val})
	case *ssa.BinOp:
		b.handleBinOp(bb, v)
	case *ssa.UnOp:
		b.handleUnOp(proc, bb, v)
	case *ssa.Convert:
		source := b.signalForValue(v.X)
		if source == nil {
			return
		}
		dest := b.ensureValueSignal(v)
		dest.Type = signalType(v.Type())
		bb.Ops = append(bb.Ops, &ConvertOperation{
			Dest:  dest,
			Value: source,
		})
	case *ssa.ChangeType:
		source := b.signalForValue(v.X)
		if source != nil {
			b.signals[v] = source
		}
	case *ssa.MakeChan:
		b.handleMakeChan(v)
	case *ssa.Send:
		b.handleSend(proc, bb, v)
	case *ssa.DebugRef:
		// Skip debug markers.
	case *ssa.Call:
		if b.handleFmtPrint(proc, bb, v) {
			return
		}
	case *ssa.Go:
		b.handleGo(proc, bb, v)
	case *ssa.IndexAddr:
		// Used for fmt.Printf variadic handling – ignore for now.
	case *ssa.MakeInterface:
		// Interfaces only appear for fmt.Printf arguments – ignore.
	case *ssa.Slice:
		// Also part of fmt formatting.
	case *ssa.If, *ssa.Jump, *ssa.Return:
		// handled separately in translateBlock
	default:
		// For unsupported instructions we emit a warning once.
		b.reporter.Warning(instr.Pos(), fmt.Sprintf("instruction %T ignored in IR builder", instr))
	}
}

func (b *builder) handleAlloc(a *ssa.Alloc) {
	ptrType, ok := a.Type().(*types.Pointer)
	if !ok {
		b.reporter.Warning(a.Pos(), "allocation without pointer type encountered")
		return
	}
	elem := ptrType.Elem()
	name := b.allocName(a)
	sig := &Signal{
		Name:   name,
		Type:   signalType(elem),
		Kind:   Reg,
		Source: a.Pos(),
	}
	b.module.Signals[sig.Name] = sig
	b.signals[a] = sig
}

func (b *builder) bindFunctionParams(fn *ssa.Function) {
	if fn == nil {
		return
	}
	for _, param := range fn.Params {
		if param == nil {
			continue
		}
		if ch, ok := b.paramChannels[param]; ok {
			b.channels[param] = ch
			continue
		}
		if sig, ok := b.paramSignals[param]; ok {
			b.signals[param] = sig
			continue
		}
		if isChannelType(param.Type()) {
			ch := &Channel{
				Name:   b.uniqueName(param.Name()),
				Type:   channelElemType(param.Type()),
				Depth:  1,
				Source: param.Pos(),
			}
			b.module.Channels[ch.Name] = ch
			b.channels[param] = ch
			b.channelUsage[ch] = 0
			continue
		}
		sig := &Signal{
			Name:   defaultName(param.Name(), b.uniqueName("param")),
			Type:   signalType(param.Type()),
			Kind:   Wire,
			Source: param.Pos(),
		}
		b.module.Signals[sig.Name] = sig
		b.signals[param] = sig
	}
}

func (b *builder) handleMakeChan(mc *ssa.MakeChan) {
	chType, ok := mc.Type().Underlying().(*types.Chan)
	if !ok {
		b.reporter.Warning(mc.Pos(), "makechan without channel type encountered")
		return
	}
	name := mc.Name()
	if name == "" {
		name = b.uniqueName("chan")
	}
	depth := 1
	if c, ok := mc.Size.(*ssa.Const); ok && c.Value != nil {
		if v, ok := constant.Int64Val(c.Value); ok && v > 0 {
			depth = int(v)
		}
	}
	channel := &Channel{
		Name:   name,
		Type:   signalType(chType.Elem()),
		Depth:  depth,
		Source: mc.Pos(),
	}
	b.module.Channels[channel.Name] = channel
	b.channels[mc] = channel
	b.channelUsage[channel] = 0
}

func (b *builder) handleSend(proc *Process, bb *BasicBlock, send *ssa.Send) {
	channel := b.channelForValue(send.Chan)
	value := b.signalForValue(send.X)
	if channel == nil || value == nil {
		return
	}
	bb.Ops = append(bb.Ops, &SendOperation{
		Channel: channel,
		Value:   value,
	})
	channel.AddEndpoint(proc, ChannelSend)
	b.recordChannelDelta(channel, 1)
}

func (b *builder) handleRecv(proc *Process, bb *BasicBlock, recv *ssa.UnOp) {
	channel := b.channelForValue(recv.X)
	dest := b.ensureValueSignal(recv)
	dest.Type = signalType(recv.Type())
	if channel == nil {
		return
	}
	bb.Ops = append(bb.Ops, &RecvOperation{
		Channel: channel,
		Dest:    dest,
	})
	channel.AddEndpoint(proc, ChannelReceive)
	b.recordChannelDelta(channel, -1)
}

func (b *builder) handleGo(proc *Process, bb *BasicBlock, stmt *ssa.Go) {
	if stmt.Call.IsInvoke() {
		b.reporter.Warning(stmt.Pos(), "interface go calls are not supported in IR builder")
		return
	}
	callee := stmt.Call.StaticCallee()
	if callee == nil {
		b.reporter.Warning(stmt.Pos(), "goroutine target has no static callee")
		return
	}
	b.bindCallArguments(callee, stmt.Call.Args)
	target := b.buildProcess(callee)
	b.assignChildStage(proc, target)
	var args []*Signal
	var chanArgs []*Channel
	var params *types.Tuple
	if sig := stmt.Call.Signature(); sig != nil {
		params = sig.Params()
	}
	for idx, arg := range stmt.Call.Args {
		var paramType types.Type
		if params != nil && idx < params.Len() {
			paramType = params.At(idx).Type()
		}
		if paramType != nil && isChannelType(paramType) {
			if ch := b.channelForValueSilent(arg); ch != nil {
				chanArgs = append(chanArgs, ch)
			}
			continue
		}
		if sig := b.signalForValue(arg); sig != nil {
			args = append(args, sig)
		}
	}
	bb.Ops = append(bb.Ops, &SpawnOperation{
		Callee:   target,
		Args:     args,
		ChanArgs: chanArgs,
	})
}

func (b *builder) bindCallArguments(fn *ssa.Function, args []ssa.Value) {
	if fn == nil {
		return
	}
	params := fn.Params
	for i := 0; i < len(params) && i < len(args); i++ {
		param := params[i]
		arg := args[i]
		paramType := param.Type()
		if isChannelType(paramType) {
			if ch := b.channelForValueSilent(arg); ch != nil {
				if _, exists := b.paramChannels[param]; !exists {
					b.paramChannels[param] = ch
				}
			}
			continue
		}
		if sig := b.signalForValue(arg); sig != nil {
			if _, exists := b.paramSignals[param]; !exists {
				b.paramSignals[param] = sig
			}
		}
	}
}
func (b *builder) buildConstSignal(c *ssa.Const) *Signal {
	sig := &Signal{
		Name:   b.newConstName(),
		Type:   signalType(c.Type()),
		Kind:   Const,
		Source: c.Pos(),
		Value:  extractConstValue(c),
	}
	b.module.Signals[sig.Name] = sig
	return sig
}

func (b *builder) signalForValue(v ssa.Value) *Signal {
	if sig, ok := b.signals[v]; ok {
		return sig
	}
	switch val := v.(type) {
	case *ssa.Const:
		sig := b.buildConstSignal(val)
		b.signals[v] = sig
		return sig
	case *ssa.BinOp:
		return b.ensureValueSignal(val)
	case *ssa.ChangeType:
		if src := b.signalForValue(val.X); src != nil {
			b.signals[v] = src
			return src
		}
	case *ssa.Phi:
		return b.ensureValueSignal(val)
	case *ssa.IndexAddr, *ssa.MakeInterface, *ssa.Slice, *ssa.MakeChan:
		return nil
	}
	b.reporter.Warning(v.Pos(), fmt.Sprintf("no signal mapping for value %T", v))
	return nil
}

func (b *builder) channelForValue(v ssa.Value) *Channel {
	return b.lookupChannel(v, true)
}

func (b *builder) channelForValueSilent(v ssa.Value) *Channel {
	return b.lookupChannel(v, false)
}

func (b *builder) lookupChannel(v ssa.Value, warn bool) *Channel {
	if ch, ok := b.channels[v]; ok {
		return ch
	}
	switch val := v.(type) {
	case *ssa.ChangeType:
		return b.lookupChannel(val.X, warn)
	}
	if warn && v != nil {
		b.reporter.Warning(v.Pos(), fmt.Sprintf("no channel mapping for value %T", v))
	}
	return nil
}

func (b *builder) newConstName() string {
	name := fmt.Sprintf("const_%d", b.tempID)
	b.tempID++
	return name
}

func (b *builder) handleFmtPrint(proc *Process, bb *BasicBlock, call *ssa.Call) bool {
	fn, ok := call.Call.Value.(*ssa.Function)
	if !ok || fn.Pkg == nil || fn.Pkg.Pkg == nil {
		return false
	}
	if fn.Pkg.Pkg.Path() != "fmt" {
		return false
	}

	var segments []PrintSegment
	var err error

	switch fn.Name() {
	case "Printf":
		if len(call.Call.Args) == 0 {
			b.reporter.Warning(call.Pos(), "fmt.Printf requires a constant format string")
			return true
		}
		formatConst, ok := call.Call.Args[0].(*ssa.Const)
		if !ok {
			b.reporter.Warning(call.Pos(), "fmt.Printf format must be a constant string")
			return true
		}
		format := constant.StringVal(formatConst.Value)
		argValues, argErr := b.expandCallArgs(call.Call.Args[1:])
		if argErr != nil {
			err = argErr
			break
		}
		segments, err = b.buildPrintfSegments(format, argValues)
	case "Println":
		segments, err = b.buildPrintSegments(call.Call.Args, true)
	case "Print":
		segments, err = b.buildPrintSegments(call.Call.Args, false)
	default:
		return false
	}

	if err != nil {
		b.reporter.Warning(call.Pos(), fmt.Sprintf("fmt.%s: %v", fn.Name(), err))
		return true
	}
	if len(segments) == 0 {
		segments = appendLiteralSegment(nil, "")
	}
	bb.Ops = append(bb.Ops, &PrintOperation{Segments: segments})
	return true
}

func (b *builder) buildPrintfSegments(format string, args []ssa.Value) ([]PrintSegment, error) {
	var segments []PrintSegment
	argIndex := 0
	var literal strings.Builder
	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		segments = appendLiteralSegment(segments, literal.String())
		literal.Reset()
	}
	for i := 0; i < len(format); {
		if format[i] != '%' {
			literal.WriteByte(format[i])
			i++
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			literal.WriteByte('%')
			i += 2
			continue
		}
		i++
		if i >= len(format) {
			return nil, fmt.Errorf("trailing %% in format string")
		}
		verbChar := format[i]
		i++
		flushLiteral()
		if argIndex >= len(args) {
			return nil, fmt.Errorf("not enough arguments for format")
		}
		sig := b.signalForValue(args[argIndex])
		if sig == nil {
			return nil, fmt.Errorf("unsupported argument type %T", args[argIndex])
		}
		argIndex++
		var verb PrintVerb
		switch verbChar {
		case 'd', 'v':
			verb = PrintVerbDec
		case 'x', 'X':
			verb = PrintVerbHex
		case 'b':
			verb = PrintVerbBin
		case 't':
			verb = PrintVerbDec
		default:
			return nil, fmt.Errorf("unsupported verb %%%c", verbChar)
		}
		segments = append(segments, PrintSegment{Value: sig, Verb: verb})
	}
	flushLiteral()
	if argIndex != len(args) {
		return nil, fmt.Errorf("too many arguments for format")
	}
	return segments, nil
}

func (b *builder) buildPrintSegments(args []ssa.Value, newline bool) ([]PrintSegment, error) {
	var segments []PrintSegment
	appendValueSegments := func(v ssa.Value) error {
		switch val := v.(type) {
		case *ssa.Const:
			if val.Value.Kind() == constant.String {
				segments = appendLiteralSegment(segments, constant.StringVal(val.Value))
				return nil
			}
		}
		sig := b.signalForValue(v)
		if sig == nil {
			return fmt.Errorf("unsupported argument %T", v)
		}
		segments = append(segments, PrintSegment{Value: sig, Verb: PrintVerbDec})
		return nil
	}
	for idx, arg := range args {
		if idx > 0 {
			segments = appendLiteralSegment(segments, " ")
		}
		if err := appendValueSegments(arg); err != nil {
			return nil, err
		}
	}
	if newline {
		segments = appendLiteralSegment(segments, "\n")
	}
	if len(segments) == 0 && newline {
		segments = appendLiteralSegment(segments, "\n")
	}
	return segments, nil
}

func appendLiteralSegment(segments []PrintSegment, text string) []PrintSegment {
	if text == "" {
		return segments
	}
	if len(segments) > 0 && segments[len(segments)-1].Value == nil {
		segments[len(segments)-1].Text += text
		return segments
	}
	return append(segments, PrintSegment{Text: text})
}

func (b *builder) expandCallArgs(args []ssa.Value) ([]ssa.Value, error) {
	var expanded []ssa.Value
	for _, arg := range args {
		if slice, ok := arg.(*ssa.Slice); ok {
			values, err := b.expandVarArgs(slice)
			if err != nil {
				return nil, err
			}
			expanded = append(expanded, values...)
			continue
		}
		expanded = append(expanded, arg)
	}
	return expanded, nil
}

func (b *builder) expandVarArgs(slice *ssa.Slice) ([]ssa.Value, error) {
	alloc, ok := slice.X.(*ssa.Alloc)
	if !ok || alloc.Comment != "varargs" {
		return nil, fmt.Errorf("unsupported variadic argument form %T", slice.X)
	}
	referrers := alloc.Referrers()
	if referrers == nil {
		return nil, fmt.Errorf("varargs slice has no referrers")
	}
	type indexedValue struct {
		index int
		value ssa.Value
	}
	var items []indexedValue
	for _, ref := range *referrers {
		idxAddr, ok := ref.(*ssa.IndexAddr)
		if !ok || idxAddr.X != alloc {
			continue
		}
		idxConst, ok := idxAddr.Index.(*ssa.Const)
		if !ok {
			continue
		}
		index64, ok := constant.Int64Val(idxConst.Value)
		if !ok {
			return nil, fmt.Errorf("non-integer vararg index")
		}
		index := int(index64)
		var stored ssa.Value
		if users := idxAddr.Referrers(); users != nil {
			for _, user := range *users {
				store, ok := user.(*ssa.Store)
				if !ok {
					continue
				}
				stored = store.Val
				break
			}
		}
		if stored == nil {
			continue
		}
		if mi, ok := stored.(*ssa.MakeInterface); ok {
			stored = mi.X
		}
		items = append(items, indexedValue{index: index, value: stored})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("failed to decode variadic arguments")
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].index < items[j].index
	})
	values := make([]ssa.Value, 0, len(items))
	for _, item := range items {
		values = append(values, item.value)
	}
	return values, nil
}

func defaultPorts() []Port {
	return []Port{
		{
			Name:      "clk",
			Direction: Input,
			Type: &SignalType{
				Width:  1,
				Signed: false,
			},
		},
		{
			Name:      "rst",
			Direction: Input,
			Type: &SignalType{
				Width:  1,
				Signed: false,
			},
		},
	}
}

func blockComment(block *ssa.BasicBlock) string {
	if block.Comment != "" {
		return block.Comment
	}
	return fmt.Sprintf("block_%d", block.Index)
}

func translateBinOp(tok token.Token) (BinOp, bool) {
	switch tok {
	case token.ADD:
		return Add, true
	case token.SUB:
		return Sub, true
	case token.MUL:
		return Mul, true
	case token.AND:
		return And, true
	case token.OR:
		return Or, true
	case token.XOR:
		return Xor, true
	case token.SHL:
		return Shl, true
	case token.SHR:
		return ShrU, true
	default:
		return 0, false
	}
}

func isShiftBinOp(op BinOp) bool {
	switch op {
	case Shl, ShrU, ShrS:
		return true
	default:
		return false
	}
}

func translateCompareOp(tok token.Token, signed bool) (ComparePredicate, bool) {
	switch tok {
	case token.EQL:
		return CompareEQ, true
	case token.NEQ:
		return CompareNE, true
	case token.LSS:
		if signed {
			return CompareSLT, true
		}
		return CompareULT, true
	case token.LEQ:
		if signed {
			return CompareSLE, true
		}
		return CompareULE, true
	case token.GTR:
		if signed {
			return CompareSGT, true
		}
		return CompareUGT, true
	case token.GEQ:
		if signed {
			return CompareSGE, true
		}
		return CompareUGE, true
	default:
		return 0, false
	}
}

func isSignedType(t types.Type) bool {
	if t == nil {
		return true
	}
	if basic, ok := t.Underlying().(*types.Basic); ok {
		if basic.Info()&types.IsUnsigned != 0 {
			return false
		}
	}
	return true
}

func signalType(t types.Type) *SignalType {
	switch bt := t.Underlying().(type) {
	case *types.Basic:
		width, signed := widthForBasic(bt)
		return &SignalType{Width: width, Signed: signed}
	default:
		return &SignalType{Width: 32, Signed: true}
	}
}

func widthForBasic(b *types.Basic) (int, bool) {
	switch b.Kind() {
	case types.Int8:
		return 8, true
	case types.Uint8:
		return 8, false
	case types.Int16:
		return 16, true
	case types.Uint16:
		return 16, false
	case types.Int32, types.Int:
		return 32, true
	case types.Uint32, types.Uint:
		return 32, false
	case types.Int64:
		return 64, true
	case types.Uint64:
		return 64, false
	case types.Bool:
		return 1, false
	default:
		return 32, true
	}
}

func isChannelType(t types.Type) bool {
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

func channelElemType(t types.Type) *SignalType {
	if ch, ok := t.Underlying().(*types.Chan); ok {
		return signalType(ch.Elem())
	}
	return &SignalType{Width: 1, Signed: false}
}

func defaultName(candidate, fallback string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return fallback
	}
	return candidate
}

func extractConstValue(c *ssa.Const) interface{} {
	if c.IsNil() {
		return nil
	}
	switch c.Type().Underlying().(*types.Basic).Kind() {
	case types.Int8, types.Int16, types.Int32, types.Int64, types.Int:
		if i, ok := constant.Int64Val(c.Value); ok {
			return i
		}
	case types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uint:
		if u, ok := constant.Uint64Val(c.Value); ok {
			return u
		}
	case types.Bool:
		return constant.BoolVal(c.Value)
	}
	return c.Value.ExactString()
}

func findMainPackage(prog *ssa.Program) *ssa.Package {
	for _, pkg := range prog.AllPackages() {
		if pkg == nil || pkg.Pkg == nil {
			continue
		}
		if pkg.Pkg.Path() == "main" || pkg.Pkg.Name() == "main" {
			return pkg
		}
	}
	return nil
}

func (b *builder) allocName(a *ssa.Alloc) string {
	candidate := strings.TrimSpace(a.Comment)
	if strings.HasPrefix(candidate, "var ") {
		candidate = strings.TrimPrefix(candidate, "var ")
	}
	if candidate == "" {
		candidate = a.Name()
	}
	if candidate == "" {
		return b.uniqueName("alloc")
	}
	candidate = strings.ReplaceAll(candidate, ".", "_")
	candidate = strings.ReplaceAll(candidate, " ", "_")
	return candidate
}

func (b *builder) uniqueName(prefix string) string {
	name := fmt.Sprintf("%s_%d", prefix, b.tempID)
	b.tempID++
	return name
}

func (b *builder) newAnonymousSignal(prefix string, typ *SignalType, pos token.Pos) *Signal {
	if prefix == "" {
		prefix = "tmp"
	}
	name := b.uniqueName(prefix)
	sig := &Signal{
		Name:   name,
		Type:   typ.Clone(),
		Kind:   Wire,
		Source: pos,
	}
	if sig.Type == nil {
		sig.Type = &SignalType{}
	}
	if b.module != nil {
		b.module.Signals[name] = sig
	}
	return sig
}

func (b *builder) ensureValueSignal(v ssa.Value) *Signal {
	if v == nil {
		return nil
	}
	if sig, ok := b.signals[v]; ok && sig != nil {
		return sig
	}
	base := defaultName(v.Name(), "tmp")
	name := b.uniqueName(base)
	sig := &Signal{
		Name:   name,
		Type:   signalType(v.Type()),
		Kind:   Wire,
		Source: v.Pos(),
	}
	if b.module != nil {
		b.module.Signals[sig.Name] = sig
	}
	b.signals[v] = sig
	return sig
}
