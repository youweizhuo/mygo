package passes

import (
	"fmt"

	"mygo/internal/diag"
	"mygo/internal/ir"
)

// WidthInference propagates width and signedness facts across the IR and
// reports implicit truncation or sign changes.
type WidthInference struct {
	reporter      *diag.Reporter
	maxIterations int
}

// NewWidthInference constructs the pass. reporter is optional but recommended
// so the pass can surface precise diagnostics.
func NewWidthInference(reporter *diag.Reporter) *WidthInference {
	return &WidthInference{
		reporter:      reporter,
		maxIterations: 32,
	}
}

// Name implements the Pass interface.
func (w *WidthInference) Name() string {
	return "width-inference"
}

// Run executes the pass over the entire design.
func (w *WidthInference) Run(design *ir.Design) error {
	if design == nil {
		return fmt.Errorf("width inference requires a non-nil design")
	}
	for _, module := range design.Modules {
		if err := w.visitModule(module); err != nil {
			return err
		}
	}
	if w.reporter != nil && w.reporter.HasErrors() {
		return fmt.Errorf("width inference reported errors")
	}
	return nil
}

func (w *WidthInference) visitModule(module *ir.Module) error {
	if module == nil {
		return nil
	}
	changed := true
	iteration := 0
	for changed {
		iteration++
		if iteration > w.maxIterations {
			return fmt.Errorf("width inference did not converge for module %s", module.Name)
		}
		changed = false
		for _, proc := range module.Processes {
			for _, block := range proc.Blocks {
				for _, op := range block.Ops {
					switch o := op.(type) {
					case *ir.AssignOperation:
						if w.propagateAssign(o) {
							changed = true
						}
					case *ir.BinOperation:
						if w.propagateBin(o) {
							changed = true
						}
					case *ir.ConvertOperation:
						changed = w.ensureTypes(o.Dest, o.Value) || changed
					case *ir.MuxOperation:
						if w.propagateMux(o) {
							changed = true
						}
					case *ir.CompareOperation:
						if w.propagateCompare(o) {
							changed = true
						}
					case *ir.NotOperation:
						if w.propagateNot(o) {
							changed = true
						}
					case *ir.PhiOperation:
						if w.propagatePhi(o) {
							changed = true
						}
					default:
						// No width effects.
					}
				}
			}
		}
	}
	return nil
}

func (w *WidthInference) propagateAssign(op *ir.AssignOperation) bool {
	if op == nil {
		return false
	}
	destType := ensureSignalType(op.Dest)
	srcType := ensureSignalType(op.Value)
	changed := false

	switch {
	case destType.IsUnknown() && !srcType.IsUnknown():
		changed = copyType(destType, srcType) || changed
	case srcType.IsUnknown() && !destType.IsUnknown():
		changed = copyType(srcType, destType) || changed
	default:
		if !srcType.FitsWithin(destType) {
			w.report(op.Value, fmt.Sprintf("assignment from %s (%s) into %s (%s) truncates value; add an explicit conversion",
				signalLabel(op.Value), srcType.Description(), signalLabel(op.Dest), destType.Description()))
		}
		if !srcType.SignedCompatible(destType) {
			w.report(op.Value, fmt.Sprintf("assignment from %s (%s) into %s (%s) changes signedness; add an explicit conversion",
				signalLabel(op.Value), srcType.Description(), signalLabel(op.Dest), destType.Description()))
		}
	}
	return changed
}

func (w *WidthInference) propagateBin(op *ir.BinOperation) bool {
	if op == nil {
		return false
	}
	leftType := ensureSignalType(op.Left)
	rightType := ensureSignalType(op.Right)
	destType := ensureSignalType(op.Dest)
	changed := false

	requiresMatchingTypes := op.Op != ir.Shl && op.Op != ir.ShrU && op.Op != ir.ShrS

	if requiresMatchingTypes {
		if leftType.IsUnknown() && !rightType.IsUnknown() {
			changed = copyType(leftType, rightType) || changed
		}
		if rightType.IsUnknown() && !leftType.IsUnknown() {
			changed = copyType(rightType, leftType) || changed
		}
	}

	if leftType.IsUnknown() || rightType.IsUnknown() {
		return changed
	}

	if requiresMatchingTypes && leftType.Signed != rightType.Signed {
		w.report(op.Left, fmt.Sprintf("mixed signed/unsigned operands (%s vs %s) in %s; insert an explicit conversion",
			leftType.Description(), rightType.Description(), signalLabel(op.Dest)))
		return changed
	}

	resultType := leftType.ResultFor(op.Op, rightType)
	if destType.IsUnknown() {
		changed = copyType(destType, resultType) || changed
		return changed
	}

	if !resultType.FitsWithin(destType) {
		w.report(op.Dest, fmt.Sprintf("%s produces %s but is declared as %s; add an explicit conversion or adjust the declaration",
			signalLabel(op.Dest), resultType.Description(), destType.Description()))
	}
	if !resultType.SignedCompatible(destType) {
		w.report(op.Dest, fmt.Sprintf("%s is inferred as %s but declared as %s; add an explicit conversion",
			signalLabel(op.Dest), resultType.Description(), destType.Description()))
	}

	return changed
}

func (w *WidthInference) propagateMux(op *ir.MuxOperation) bool {
	if op == nil {
		return false
	}
	changed := false
	condType := ensureSignalType(op.Cond)
	if condType.Width != 1 || condType.Signed {
		condType.Width = 1
		condType.Signed = false
		changed = true
	}
	trueType := ensureSignalType(op.TrueValue)
	falseType := ensureSignalType(op.FalseValue)
	destType := ensureSignalType(op.Dest)

	if trueType.IsUnknown() && !falseType.IsUnknown() {
		changed = copyType(trueType, falseType) || changed
	} else if falseType.IsUnknown() && !trueType.IsUnknown() {
		changed = copyType(falseType, trueType) || changed
	}

	if trueType.IsUnknown() || falseType.IsUnknown() {
		return changed
	}

	if !trueType.Equal(falseType) {
		w.report(op.Dest, fmt.Sprintf("mux inputs have mismatched types (%s vs %s)",
			trueType.Description(), falseType.Description()))
		return changed
	}

	if destType.IsUnknown() {
		changed = copyType(destType, trueType) || changed
		return changed
	}

	if !trueType.FitsWithin(destType) {
		w.report(op.Dest, fmt.Sprintf("mux result %s does not fit destination %s",
			trueType.Description(), destType.Description()))
	}
	if !trueType.SignedCompatible(destType) {
		w.report(op.Dest, fmt.Sprintf("mux result signedness (%s) mismatches destination %s",
			trueType.Description(), destType.Description()))
	}
	return changed
}

func (w *WidthInference) propagateCompare(op *ir.CompareOperation) bool {
	if op == nil {
		return false
	}
	leftType := ensureSignalType(op.Left)
	rightType := ensureSignalType(op.Right)
	destType := ensureSignalType(op.Dest)
	changed := false

	if leftType.IsUnknown() && !rightType.IsUnknown() {
		changed = copyType(leftType, rightType) || changed
	} else if rightType.IsUnknown() && !leftType.IsUnknown() {
		changed = copyType(rightType, leftType) || changed
	}

	if leftType.IsUnknown() || rightType.IsUnknown() {
		return changed
	}

	if leftType.Width != rightType.Width {
		w.report(op.Left, fmt.Sprintf("compare operands %s vs %s have mismatched widths",
			leftType.Description(), rightType.Description()))
	}
	if leftType.Signed != rightType.Signed {
		w.report(op.Left, fmt.Sprintf("compare operands %s vs %s have mismatched signedness",
			leftType.Description(), rightType.Description()))
	}

	if destType.Width != 1 || destType.Signed {
		destType.Width = 1
		destType.Signed = false
		changed = true
	}
	return changed
}

func (w *WidthInference) propagateNot(op *ir.NotOperation) bool {
	if op == nil {
		return false
	}
	valType := ensureSignalType(op.Value)
	destType := ensureSignalType(op.Dest)
	changed := false
	if destType.IsUnknown() && !valType.IsUnknown() {
		changed = copyType(destType, valType) || changed
		return changed
	}
	if !valType.IsUnknown() && !destType.IsUnknown() {
		if !valType.Equal(destType) {
			w.report(op.Dest, fmt.Sprintf("not operand %s differs from destination %s",
				valType.Description(), destType.Description()))
		}
	}
	return changed
}

func (w *WidthInference) propagatePhi(op *ir.PhiOperation) bool {
	if op == nil {
		return false
	}
	destType := ensureSignalType(op.Dest)
	var ref *ir.SignalType
	changed := false
	for _, incoming := range op.Incomings {
		inType := ensureSignalType(incoming.Value)
		if ref == nil && !inType.IsUnknown() {
			ref = inType
		}
	}
	if ref == nil {
		return changed
	}
	for _, incoming := range op.Incomings {
		inType := ensureSignalType(incoming.Value)
		if inType.IsUnknown() {
			changed = copyType(inType, ref) || changed
			continue
		}
		if !inType.Equal(ref) {
			w.report(incoming.Value, fmt.Sprintf("phi inputs mismatch (%s vs %s)",
				inType.Description(), ref.Description()))
		}
	}
	if destType.IsUnknown() {
		changed = copyType(destType, ref) || changed
		return changed
	}
	if !ref.FitsWithin(destType) {
		w.report(op.Dest, fmt.Sprintf("phi result %s does not fit destination %s",
			ref.Description(), destType.Description()))
	}
	if !ref.SignedCompatible(destType) {
		w.report(op.Dest, fmt.Sprintf("phi result signedness %s mismatches destination %s",
			ref.Description(), destType.Description()))
	}
	return changed
}
func (w *WidthInference) ensureTypes(signals ...*ir.Signal) bool {
	changed := false
	for _, sig := range signals {
		if sig == nil {
			continue
		}
		if sig.Type == nil {
			sig.Type = &ir.SignalType{}
			changed = true
		}
	}
	return changed
}

func (w *WidthInference) report(sig *ir.Signal, msg string) {
	if w.reporter == nil {
		return
	}
	if sig != nil {
		w.reporter.Error(sig.Source, msg)
		return
	}
	w.reporter.Errorf("%s", msg)
}

func ensureSignalType(sig *ir.Signal) *ir.SignalType {
	if sig == nil {
		return &ir.SignalType{}
	}
	if sig.Type == nil {
		sig.Type = &ir.SignalType{}
	}
	return sig.Type
}

func copyType(dst, src *ir.SignalType) bool {
	if dst == nil || src == nil || src.IsUnknown() {
		return false
	}
	changed := false
	if dst.Width != src.Width {
		dst.Width = src.Width
		changed = true
	}
	if dst.Signed != src.Signed {
		dst.Signed = src.Signed
		changed = true
	}
	return changed
}

func signalLabel(sig *ir.Signal) string {
	if sig == nil {
		return "value"
	}
	if sig.Name == "" {
		return "value"
	}
	return fmt.Sprintf("signal %q", sig.Name)
}
