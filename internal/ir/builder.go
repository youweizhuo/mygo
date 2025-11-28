package ir

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
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
	b.buildProcess(fn)

	return mod
}

func (b *builder) buildProcess(fn *ssa.Function) *Process {
	if proc, ok := b.processes[fn]; ok {
		return proc
	}
	proc := &Process{
		Name:        fn.Name(),
		Sensitivity: Sequential,
	}
	b.processes[fn] = proc
	b.module.Processes = append(b.module.Processes, proc)
	b.bindFunctionParams(fn)

	for _, block := range fn.Blocks {
		if block == nil {
			continue
		}
		bb := &BasicBlock{Label: blockComment(block)}
		for _, instr := range block.Instrs {
			b.translateInstr(proc, bb, instr)
		}
		proc.Blocks = append(proc.Blocks, bb)
	}
	return proc
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
		left := b.signalForValue(v.X)
		right := b.signalForValue(v.Y)
		if left == nil || right == nil {
			return
		}
		dest := b.newTempSignal(v.Name(), v.Type(), v.Pos())
		op, ok := translateBinOp(v.Op)
		if !ok {
			b.reporter.Errorf("unsupported binary op: %s", v.Op.String())
			return
		}
		bb.Ops = append(bb.Ops, &BinOperation{
			Op:    op,
			Dest:  dest,
			Left:  left,
			Right: right,
		})
		b.signals[v] = dest
	case *ssa.UnOp:
		switch v.Op {
		case token.MUL:
			ptr := b.signalForValue(v.X)
			if ptr != nil {
				b.signals[v] = ptr
			}
		case token.ARROW:
			b.handleRecv(proc, bb, v)
		}
	case *ssa.Convert:
		source := b.signalForValue(v.X)
		if source == nil {
			return
		}
		dest := b.newTempSignal(v.Name(), v.Type(), v.Pos())
		b.signals[v] = dest
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
	case *ssa.Return:
		// No hardware action needed for now.
	case *ssa.DebugRef:
		// Skip debug markers.
	case *ssa.Call:
		// Calls such as fmt.Printf are host-side only for Phase 1; ignore.
	case *ssa.Go:
		b.handleGo(proc, bb, v)
	case *ssa.IndexAddr:
		// Used for fmt.Printf variadic handling – ignore for now.
	case *ssa.MakeInterface:
		// Interfaces only appear for fmt.Printf arguments – ignore.
	case *ssa.Slice:
		// Also part of fmt formatting.
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
}

func (b *builder) handleRecv(proc *Process, bb *BasicBlock, recv *ssa.UnOp) {
	channel := b.channelForValue(recv.X)
	dest := b.newTempSignal(recv.Name(), recv.Type(), recv.Pos())
	b.signals[recv] = dest
	if channel == nil {
		return
	}
	bb.Ops = append(bb.Ops, &RecvOperation{
		Channel: channel,
		Dest:    dest,
	})
	channel.AddEndpoint(proc, ChannelReceive)
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
	case *ssa.ChangeType:
		if src := b.signalForValue(val.X); src != nil {
			b.signals[v] = src
			return src
		}
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

func (b *builder) newTempSignal(base string, typ types.Type, pos token.Pos) *Signal {
	if base == "" {
		base = fmt.Sprintf("tmp%d", b.tempID)
	} else {
		base = fmt.Sprintf("%s_%d", base, b.tempID)
	}
	b.tempID++
	name := base
	sig := &Signal{
		Name:   name,
		Type:   signalType(typ),
		Kind:   Wire,
		Source: pos,
	}
	b.module.Signals[sig.Name] = sig
	return sig
}

func (b *builder) newConstName() string {
	name := fmt.Sprintf("const_%d", b.tempID)
	b.tempID++
	return name
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
	default:
		return 0, false
	}
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
