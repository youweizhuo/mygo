package mlir

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"mygo/internal/ir"
)

// Emit writes the MLIR representation of the design to outputPath. When
// outputPath is empty or "-", the result is written to stdout.
func Emit(design *ir.Design, outputPath string) error {
	var w io.Writer
	if outputPath == "" || outputPath == "-" {
		w = os.Stdout
	} else {
		f, err := os.Create(outputPath)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}

	pr := &printer{
		w:          w,
		constNames: make(map[*ir.Signal]string),
		valueNames: make(map[*ir.Signal]string),
		portNames:  make(map[string]string),
	}

	fmt.Fprintln(w, "module {")
	pr.indent++
	for _, module := range design.Modules {
		pr.emitModule(module)
	}
	pr.indent--
	fmt.Fprintln(w, "}")
	return nil
}

type printer struct {
	w          io.Writer
	indent     int
	nextTemp   int
	constNames map[*ir.Signal]string
	valueNames map[*ir.Signal]string
	portNames  map[string]string
}

func (p *printer) emitModule(module *ir.Module) {
	p.printIndent()
	fmt.Fprintf(p.w, "hw.module @%s(", module.Name)
	inputs, outputs := portLists(module.Ports)
	for i, in := range inputs {
		if i > 0 {
			fmt.Fprint(p.w, ", ")
		}
		fmt.Fprint(p.w, in)
	}
	fmt.Fprint(p.w, ")")
	if len(outputs) > 0 {
		fmt.Fprint(p.w, " -> (")
		for i, out := range outputs {
			if i > 0 {
				fmt.Fprint(p.w, ", ")
			}
			fmt.Fprint(p.w, out)
		}
		fmt.Fprint(p.w, ")")
	}
	fmt.Fprintln(p.w, " {")
	p.indent++

	p.resetModuleState(module.Ports)
	p.emitConstants(module)
	p.emitChannels(module)
	p.emitProcesses(module)

	p.printIndent()
	fmt.Fprintln(p.w, "hw.output")

	p.indent--
	p.printIndent()
	fmt.Fprintln(p.w, "}")
}

func (p *printer) emitConstants(module *ir.Module) {
	names := make([]string, 0, len(module.Signals))
	for name, sig := range module.Signals {
		if sig.Kind == ir.Const {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		sig := module.Signals[name]
		ssaName := p.assignConst(sig)
		p.printIndent()
		fmt.Fprintf(p.w, "%s = hw.constant %v : %s\n", ssaName, sig.Value, typeString(sig.Type))
	}
}

func (p *printer) emitChannels(module *ir.Module) {
	if len(module.Channels) == 0 {
		return
	}
	names := make([]string, 0, len(module.Channels))
	for name := range module.Channels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ch := module.Channels[name]
		p.printIndent()
		fmt.Fprintf(p.w, "// channel %s depth=%d type=%s\n", ch.Name, ch.Depth, typeString(ch.Type))
	}
}

func (p *printer) emitProcesses(module *ir.Module) {
	for _, proc := range module.Processes {
		for _, block := range proc.Blocks {
			for _, op := range block.Ops {
				p.emitOperation(op, proc)
			}
		}
	}
}

func (p *printer) emitOperation(op ir.Operation, proc *ir.Process) {
	switch o := op.(type) {
	case *ir.BinOperation:
		left := p.valueRef(o.Left)
		right := p.valueRef(o.Right)
		dest := p.bindSSA(o.Dest)
		p.printIndent()
		fmt.Fprintf(p.w, "%s = comb.%s %s, %s : %s\n",
			dest,
			binOpName(o.Op),
			left,
			right,
			typeString(o.Dest.Type),
		)
	case *ir.ConvertOperation:
		src := p.valueRef(o.Value)
		dest := p.bindSSA(o.Dest)
		from := typeString(o.Value.Type)
		to := typeString(o.Dest.Type)
		p.printIndent()
		if o.Value.Type.Width == o.Dest.Type.Width {
			fmt.Fprintf(p.w, "%s = comb.bitcast %s : %s -> %s\n", dest, src, from, to)
		} else if o.Value.Type.Signed {
			fmt.Fprintf(p.w, "%s = comb.sext %s : %s to %s\n", dest, src, from, to)
		} else {
			fmt.Fprintf(p.w, "%s = comb.zext %s : %s to %s\n", dest, src, from, to)
		}
	case *ir.AssignOperation:
		clk := p.portRef("clk")
		src := p.valueRef(o.Value)
		dest := p.bindSSA(o.Dest)
		if clk == "" {
			clk = "%clk"
		}
		p.printIndent()
		fmt.Fprintf(p.w, "%s = seq.compreg %s, %s : %s\n", dest, src, clk, typeString(o.Dest.Type))
	case *ir.SendOperation:
		value := p.valueRef(o.Value)
		p.printIndent()
		fmt.Fprintf(p.w, "mygo.channel.send \"%s\"(%s) : %s\n", sanitize(o.Channel.Name), value, typeString(o.Value.Type))
	case *ir.RecvOperation:
		dest := p.bindSSA(o.Dest)
		p.printIndent()
		fmt.Fprintf(p.w, "%s = mygo.channel.recv \"%s\" : %s\n", dest, sanitize(o.Channel.Name), typeString(o.Dest.Type))
	case *ir.SpawnOperation:
		args := make([]string, 0, len(o.Args))
		for _, arg := range o.Args {
			args = append(args, p.valueRef(arg))
		}
		chanNames := make([]string, 0, len(o.ChanArgs))
		for _, ch := range o.ChanArgs {
			chanNames = append(chanNames, fmt.Sprintf("\"%s\"", sanitize(ch.Name)))
		}
		argList := strings.Join(args, ", ")
		chanList := strings.Join(chanNames, ", ")
		p.printIndent()
		if len(chanNames) > 0 {
			fmt.Fprintf(p.w, "mygo.process.spawn \"%s\"(%s) channels [%s]\n", sanitize(o.Callee.Name), argList, chanList)
		} else {
			fmt.Fprintf(p.w, "mygo.process.spawn \"%s\"(%s)\n", sanitize(o.Callee.Name), argList)
		}
	case *ir.CompareOperation:
		left := p.valueRef(o.Left)
		right := p.valueRef(o.Right)
		dest := p.bindSSA(o.Dest)
		operandType := typeString(o.Left.Type)
		p.printIndent()
		fmt.Fprintf(p.w, "%s = comb.icmp %s, %s, %s : %s\n",
			dest,
			comparePredicateName(o.Predicate),
			left,
			right,
			operandType,
		)
	case *ir.NotOperation:
		value := p.valueRef(o.Value)
		dest := p.bindSSA(o.Dest)
		p.printIndent()
		fmt.Fprintf(p.w, "%s = comb.not %s : %s\n", dest, value, typeString(o.Value.Type))
	case *ir.MuxOperation:
		cond := p.valueRef(o.Cond)
		tVal := p.valueRef(o.TrueValue)
		fVal := p.valueRef(o.FalseValue)
		dest := p.bindSSA(o.Dest)
		p.printIndent()
		fmt.Fprintf(p.w, "%s = comb.mux %s, %s, %s : %s, %s\n",
			dest,
			cond,
			tVal,
			fVal,
			typeString(o.Cond.Type),
			typeString(o.Dest.Type),
		)
	case *ir.PhiOperation:
		p.printIndent()
		fmt.Fprintf(p.w, "// phi %s has %d incoming values\n", sanitize(o.Dest.Name), len(o.Incomings))
	default:
		// skip unknown operations for now
	}
}

func (p *printer) resetModuleState(ports []ir.Port) {
	p.constNames = make(map[*ir.Signal]string)
	p.valueNames = make(map[*ir.Signal]string)
	p.portNames = make(map[string]string)
	for _, port := range ports {
		name := "%" + sanitize(port.Name)
		p.portNames[port.Name] = name
	}
}

func (p *printer) assignConst(sig *ir.Signal) string {
	if name, ok := p.constNames[sig]; ok {
		return name
	}
	name := fmt.Sprintf("%%c%d", p.nextTemp)
	p.nextTemp++
	p.constNames[sig] = name
	return name
}

func (p *printer) bindSSA(sig *ir.Signal) string {
	if sig == nil {
		return "%unknown"
	}
	if name, ok := p.valueNames[sig]; ok {
		return name
	}
	name := fmt.Sprintf("%%v%d", p.nextTemp)
	p.nextTemp++
	p.valueNames[sig] = name
	return name
}

func (p *printer) valueRef(sig *ir.Signal) string {
	if sig == nil {
		return "%unknown"
	}
	if sig.Kind == ir.Const {
		return p.assignConst(sig)
	}
	if name, ok := p.valueNames[sig]; ok {
		return name
	}
	name := "%" + sanitize(sig.Name)
	p.valueNames[sig] = name
	return name
}

func (p *printer) portRef(name string) string {
	return p.portNames[name]
}

func (p *printer) printIndent() {
	for i := 0; i < p.indent; i++ {
		fmt.Fprint(p.w, "  ")
	}
}

func portLists(ports []ir.Port) (inputs []string, outputs []string) {
	for _, port := range ports {
		entry := fmt.Sprintf("%%%s: %s", sanitize(port.Name), typeString(port.Type))
		switch port.Direction {
		case ir.Output:
			outputs = append(outputs, entry)
		default:
			inputs = append(inputs, entry)
		}
	}
	return
}

func typeString(t *ir.SignalType) string {
	width := 1
	if t != nil && t.Width > 0 {
		width = t.Width
	}
	return fmt.Sprintf("i%d", width)
}

func binOpName(op ir.BinOp) string {
	switch op {
	case ir.Add:
		return "add"
	case ir.Sub:
		return "sub"
	case ir.Mul:
		return "mul"
	case ir.And:
		return "and"
	case ir.Or:
		return "or"
	case ir.Xor:
		return "xor"
	default:
		return "unknown"
	}
}

func comparePredicateName(pred ir.ComparePredicate) string {
	switch pred {
	case ir.CompareEQ:
		return "eq"
	case ir.CompareNE:
		return "ne"
	case ir.CompareSLT:
		return "slt"
	case ir.CompareSLE:
		return "sle"
	case ir.CompareSGT:
		return "sgt"
	case ir.CompareSGE:
		return "sge"
	case ir.CompareULT:
		return "ult"
	case ir.CompareULE:
		return "ule"
	case ir.CompareUGT:
		return "ugt"
	case ir.CompareUGE:
		return "uge"
	default:
		return "eq"
	}
}

func sanitize(name string) string {
	if name == "" {
		return "unnamed"
	}
	var b strings.Builder
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9' && i > 0) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
