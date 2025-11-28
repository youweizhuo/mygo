package validate

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"mygo/internal/diag"
)

// CheckProgram validates that the SSA program only uses the supported subset
// of Go constructs required by the deterministic concurrency model.
func CheckProgram(prog *ssa.Program, pkgs []*ssa.Package, astPkgs []*packages.Package, reporter *diag.Reporter) error {
	if prog == nil {
		return fmt.Errorf("no SSA program provided for validation")
	}
	if reporter == nil {
		return fmt.Errorf("no reporter provided for validation")
	}

	c := &checker{
		reporter:   reporter,
		allowedPkg: make(map[*ssa.Package]struct{}),
		astPkgs:    astPkgs,
	}
	for _, pkg := range pkgs {
		if pkg != nil {
			c.allowedPkg[pkg] = struct{}{}
		}
	}
	c.run(prog)
	if c.errCount > 0 {
		return fmt.Errorf("validation failed with %d issue(s)", c.errCount)
	}
	return nil
}

type checker struct {
	reporter   *diag.Reporter
	errCount   int
	allowedPkg map[*ssa.Package]struct{}
	astPkgs    []*packages.Package
}

func (c *checker) run(prog *ssa.Program) {
	c.checkASTLoops()
	for fn := range ssautil.AllFunctions(prog) {
		if fn == nil || len(fn.Blocks) == 0 {
			continue
		}
		if fn.Pkg == nil {
			continue
		}
		if len(c.allowedPkg) > 0 {
			if _, ok := c.allowedPkg[fn.Pkg]; !ok {
				continue
			}
		}
		if fn.Pkg.Pkg == nil {
			continue
		}
		c.checkFunction(fn)
	}
}

func (c *checker) checkFunction(fn *ssa.Function) {
	loopBlocks := findLoopBlocks(fn)
	for _, block := range fn.Blocks {
		if block == nil {
			continue
		}
		inLoop := loopBlocks[block]
		for _, instr := range block.Instrs {
			c.inspectInstruction(fn, block, instr, inLoop)
		}
	}
}

func (c *checker) inspectInstruction(fn *ssa.Function, block *ssa.BasicBlock, instr ssa.Instruction, inLoop bool) {
	switch inst := instr.(type) {
	case *ssa.Go:
		c.checkGo(fn, inst, inLoop)
	case *ssa.Call:
		c.checkCall(fn, inst)
	case *ssa.MakeChan:
		c.checkMakeChan(inst)
	case *ssa.Select:
		c.error(inst.Pos(), "select statements are not supported; rewrite using deterministic channel handshakes")
	case *ssa.MakeMap, *ssa.MapUpdate, *ssa.Lookup:
		c.error(inst.Pos(), "maps are not supported in hardware pipelines")
	}
}

func (c *checker) checkASTLoops() {
	if len(c.astPkgs) == 0 {
		return
	}
	for _, pkg := range c.astPkgs {
		if pkg == nil {
			continue
		}
		info := pkg.TypesInfo
		for _, file := range pkg.Syntax {
			if file == nil {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				forStmt, ok := n.(*ast.ForStmt)
				if !ok {
					return true
				}
				if !isBoundedFor(forStmt, info) {
					c.error(forStmt.For, "for loops must have compile-time constant init, condition, and step")
				}
				return true
			})
		}
	}
}

func (c *checker) checkGo(current *ssa.Function, call *ssa.Go, inLoop bool) {
	if inLoop {
		c.error(call.Pos(), "goroutines created inside loops are unsupported; unroll the loop or spawn a fixed number of processes")
		return
	}
	if call.Call.IsInvoke() {
		c.error(call.Pos(), "goroutine targets must be named functions; interface invocations are not allowed")
		return
	}
	callee := call.Call.StaticCallee()
	fnValue, ok := call.Call.Value.(*ssa.Function)
	if callee == nil || !ok {
		c.error(call.Pos(), "goroutine targets must be named functions without captures")
		return
	}
	if callee.Object() == nil || fnValue.Object() == nil {
		c.error(call.Pos(), "goroutine target %q is not a named function", callee.Name())
	}
}

func (c *checker) checkCall(current *ssa.Function, call *ssa.Call) {
	if call.Call.IsInvoke() {
		c.error(call.Pos(), "interface method calls are not supported")
		return
	}
	if callee := call.Call.StaticCallee(); callee != nil && callee == current {
		c.error(call.Pos(), "recursion is not supported; refactor %s to an iterative form", current.Name())
	}
}

func (c *checker) checkMakeChan(mc *ssa.MakeChan) {
	if mc.Size == nil {
		c.error(mc.Pos(), "channels must declare a constant capacity > 0")
		return
	}
	sizeConst, ok := mc.Size.(*ssa.Const)
	if !ok {
		c.error(mc.Pos(), "channel capacity must be a compile-time constant; got %s", describeValue(mc.Size))
	} else if sizeConst.Value == nil {
		c.error(mc.Pos(), "channel capacity must be a non-zero constant")
	} else if capVal, ok := constant.Int64Val(sizeConst.Value); !ok || capVal <= 0 {
		c.error(mc.Pos(), "channel capacity must be a positive constant; got %s", sizeConst.Value.ExactString())
	}

	elem := channelElem(mc.Type())
	if elem == nil {
		return
	}
	if !supportedChannelElem(elem) {
		c.error(mc.Pos(), "channel element type %s is not supported; only integers or fixed-size arrays of integers are allowed", elem.String())
	}
}

func (c *checker) error(pos token.Pos, format string, args ...any) {
	c.errCount++
	if c.reporter != nil {
		c.reporter.Error(pos, fmt.Sprintf(format, args...))
	}
}

func describeValue(v ssa.Value) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", v)
}

func channelElem(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	ch, _ := t.Underlying().(*types.Chan)
	if ch == nil {
		return nil
	}
	return ch.Elem()
}

func supportedChannelElem(t types.Type) bool {
	switch tt := t.Underlying().(type) {
	case *types.Basic:
		if tt.Info()&types.IsInteger != 0 {
			return true
		}
		return tt.Kind() == types.Bool
	case *types.Array:
		return supportedChannelElem(tt.Elem())
	default:
		return false
	}
}

func findLoopBlocks(fn *ssa.Function) map[*ssa.BasicBlock]bool {
	result := make(map[*ssa.BasicBlock]bool)
	index := 0
	stack := make([]*ssa.BasicBlock, 0, len(fn.Blocks))
	indices := make(map[*ssa.BasicBlock]int)
	lowlink := make(map[*ssa.BasicBlock]int)
	onStack := make(map[*ssa.BasicBlock]bool)

	var strongConnect func(v *ssa.BasicBlock)
	strongConnect = func(v *ssa.BasicBlock) {
		if v == nil {
			return
		}
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, succ := range v.Succs {
			if succ == nil {
				continue
			}
			if _, ok := indices[succ]; !ok {
				strongConnect(succ)
				if lowlink[succ] < lowlink[v] {
					lowlink[v] = lowlink[succ]
				}
			} else if onStack[succ] && indices[succ] < lowlink[v] {
				lowlink[v] = indices[succ]
			}
		}

		if lowlink[v] == indices[v] {
			component := make([]*ssa.BasicBlock, 0)
			for {
				if len(stack) == 0 {
					break
				}
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				component = append(component, w)
				if w == v {
					break
				}
			}
			if len(component) > 1 {
				for _, blk := range component {
					result[blk] = true
				}
			} else if hasSelfLoop(v) {
				result[v] = true
			}
		}
	}

	for _, block := range fn.Blocks {
		if block == nil {
			continue
		}
		if _, seen := indices[block]; !seen {
			strongConnect(block)
		}
	}
	return result
}

func hasSelfLoop(block *ssa.BasicBlock) bool {
	for _, succ := range block.Succs {
		if succ == block {
			return true
		}
	}
	return false
}

func blockPosition(block *ssa.BasicBlock) token.Pos {
	if block == nil {
		return token.NoPos
	}
	for _, instr := range block.Instrs {
		if instr == nil {
			continue
		}
		if pos := instr.Pos(); pos != token.NoPos {
			return pos
		}
	}
	return token.NoPos
}

func isBoundedFor(stmt *ast.ForStmt, info *types.Info) bool {
	if stmt == nil || stmt.Init == nil || stmt.Cond == nil || stmt.Post == nil {
		return false
	}
	iterName, _, ok := loopInitInfo(stmt.Init, info)
	if !ok {
		return false
	}
	_, direction, ok := loopConditionInfo(stmt.Cond, iterName, info)
	if !ok {
		return false
	}
	step, ok := loopStepInfo(stmt.Post, iterName, info)
	if !ok {
		return false
	}
	if direction == increasing && step <= 0 {
		return false
	}
	if direction == decreasing && step >= 0 {
		return false
	}
	return true
}

func loopInitInfo(stmt ast.Stmt, info *types.Info) (string, int64, bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return "", 0, false
	}
	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || ident.Name == "_" {
		return "", 0, false
	}
	val, ok := constantIntValue(info, assign.Rhs[0])
	if !ok {
		return "", 0, false
	}
	return ident.Name, val, true
}

type loopDirection int

const (
	increasing loopDirection = 1
	decreasing loopDirection = -1
)

func loopConditionInfo(expr ast.Expr, iter string, info *types.Info) (int64, loopDirection, bool) {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return 0, 0, false
	}
	left, ok := bin.X.(*ast.Ident)
	if !ok || left.Name != iter {
		return 0, 0, false
	}
	value, ok := constantIntValue(info, bin.Y)
	if !ok {
		return 0, 0, false
	}
	switch bin.Op {
	case token.LSS, token.LEQ:
		return value, increasing, true
	case token.GTR, token.GEQ:
		return value, decreasing, true
	default:
		return 0, 0, false
	}
}

func loopStepInfo(stmt ast.Stmt, iter string, info *types.Info) (int64, bool) {
	switch s := stmt.(type) {
	case *ast.IncDecStmt:
		ident, ok := s.X.(*ast.Ident)
		if !ok || ident.Name != iter {
			return 0, false
		}
		if s.Tok == token.INC {
			return 1, true
		}
		if s.Tok == token.DEC {
			return -1, true
		}
	case *ast.AssignStmt:
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
			return 0, false
		}
		ident, ok := s.Lhs[0].(*ast.Ident)
		if !ok || ident.Name != iter {
			return 0, false
		}
		switch s.Tok {
		case token.ADD_ASSIGN:
			return extractStepValue(s.Rhs[0], info)
		case token.SUB_ASSIGN:
			if step, ok := extractStepValue(s.Rhs[0], info); ok {
				return -step, true
			}
		case token.ASSIGN:
			bin, ok := s.Rhs[0].(*ast.BinaryExpr)
			if !ok {
				return 0, false
			}
			left, ok := bin.X.(*ast.Ident)
			if !ok || left.Name != iter {
				return 0, false
			}
			step, ok := extractStepValue(bin.Y, info)
			if !ok {
				return 0, false
			}
			switch bin.Op {
			case token.ADD:
				return step, true
			case token.SUB:
				return -step, true
			}
		}
	}
	return 0, false
}

func extractStepValue(expr ast.Expr, info *types.Info) (int64, bool) {
	return constantIntValue(info, expr)
}

func constantIntValue(info *types.Info, expr ast.Expr) (int64, bool) {
	if info == nil {
		return 0, false
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if obj, ok := info.ObjectOf(ident).(*types.Const); ok && obj.Val() != nil {
			return constant.Int64Val(obj.Val())
		}
	}
	tv, ok := info.Types[expr]
	if !ok || tv.Value == nil {
		if call, ok := expr.(*ast.CallExpr); ok && len(call.Args) == 1 {
			return constantIntValue(info, call.Args[0])
		}
		return 0, false
	}
	return constant.Int64Val(tv.Value)
}
