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
		reporter: reporter,
		signals:  make(map[ssa.Value]*Signal),
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
	reporter *diag.Reporter
	module   *Module
	process  *Process
	signals  map[ssa.Value]*Signal
	tempID   int
}

func (b *builder) buildModule(fn *ssa.Function) *Module {
	mod := &Module{
		Name:    fn.Name(),
		Ports:   defaultPorts(),
		Signals: make(map[string]*Signal),
		Source:  fn.Pos(),
	}
	b.module = mod
	b.process = &Process{Sensitivity: Sequential}
	mod.Processes = append(mod.Processes, b.process)

	for _, block := range fn.Blocks {
		if block == nil {
			continue
		}
		bb := &BasicBlock{Label: blockComment(block)}
		for _, instr := range block.Instrs {
			b.translateInstr(bb, instr)
		}
		b.process.Blocks = append(b.process.Blocks, bb)
	}

	return mod
}

func (b *builder) translateInstr(bb *BasicBlock, instr ssa.Instruction) {
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
		if v.Op == token.MUL {
			ptr := b.signalForValue(v.X)
			if ptr != nil {
				b.signals[v] = ptr
			}
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
	case *ssa.Return:
		// No hardware action needed for now.
	case *ssa.DebugRef:
		// Skip debug markers.
	case *ssa.Call:
		// Calls such as fmt.Printf are host-side only for Phase 1; ignore.
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
	case *ssa.IndexAddr, *ssa.MakeInterface, *ssa.Slice:
		return nil
	}
	b.reporter.Warning(v.Pos(), fmt.Sprintf("no signal mapping for value %T", v))
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
