package ir

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Dump writes a simple human-readable representation of the design.
func Dump(design *Design, w io.Writer) {
	if design == nil {
		fmt.Fprintln(w, "<nil design>")
		return
	}
	for _, module := range design.Modules {
		fmt.Fprintf(w, "module %s\n", module.Name)
		dumpPorts(module, w)
		dumpSignals(module, w)
		dumpChannels(module, w)
		dumpProcesses(module, w)
		fmt.Fprintln(w)
	}
}

func dumpPorts(module *Module, w io.Writer) {
	if len(module.Ports) == 0 {
		return
	}
	fmt.Fprintln(w, "  ports:")
	for _, port := range module.Ports {
		fmt.Fprintf(w, "    %s %s %db%s\n",
			portDirection(port.Direction),
			port.Name,
			port.Type.Width,
			signSuffix(port.Type.Signed),
		)
	}
}

func dumpSignals(module *Module, w io.Writer) {
	if len(module.Signals) == 0 {
		return
	}
	fmt.Fprintln(w, "  signals:")
	names := make([]string, 0, len(module.Signals))
	for name := range module.Signals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sig := module.Signals[name]
		value := ""
		if sig.Kind == Const && sig.Value != nil {
			value = fmt.Sprintf(" = %v", sig.Value)
		}
		fmt.Fprintf(w, "    %-8s %-5s %db%s%s\n",
			sig.Name,
			signalKind(sig.Kind),
			sig.Type.Width,
			signSuffix(sig.Type.Signed),
			value,
		)
	}
}

func dumpChannels(module *Module, w io.Writer) {
	if len(module.Channels) == 0 {
		return
	}
	fmt.Fprintln(w, "  channels:")
	names := make([]string, 0, len(module.Channels))
	for name := range module.Channels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ch := module.Channels[name]
		fmt.Fprintf(w, "    %-8s depth=%d type=%s\n",
			ch.Name,
			ch.Depth,
			ch.Type.Description(),
		)
	}
}

func dumpProcesses(module *Module, w io.Writer) {
	for idx, proc := range module.Processes {
		fmt.Fprintf(w, "  process %d %s (%s)\n", idx, proc.Name, sensitivity(proc.Sensitivity))
		for _, block := range proc.Blocks {
			fmt.Fprintf(w, "    block %s\n", block.Label)
			for _, op := range block.Ops {
				fmt.Fprintf(w, "      %s\n", renderOp(op))
			}
		}
	}
}

func renderOp(op Operation) string {
	switch o := op.(type) {
	case *AssignOperation:
		return fmt.Sprintf("%s := %s", o.Dest.Name, o.Value.Name)
	case *ConvertOperation:
		return fmt.Sprintf("%s := convert(%s)", o.Dest.Name, o.Value.Name)
	case *BinOperation:
		return fmt.Sprintf("%s := %s %s %s", o.Dest.Name, o.Left.Name, binOpSymbol(o.Op), o.Right.Name)
	case *SendOperation:
		return fmt.Sprintf("send %s <- %s", o.Channel.Name, o.Value.Name)
	case *RecvOperation:
		return fmt.Sprintf("%s <- %s", o.Dest.Name, o.Channel.Name)
	case *SpawnOperation:
		argNames := make([]string, 0, len(o.Args))
		for _, arg := range o.Args {
			argNames = append(argNames, arg.Name)
		}
		chanNames := make([]string, 0, len(o.ChanArgs))
		for _, ch := range o.ChanArgs {
			chanNames = append(chanNames, ch.Name)
		}
		segments := make([]string, 0, 2)
		if len(argNames) > 0 {
			segments = append(segments, strings.Join(argNames, ", "))
		}
		if len(chanNames) > 0 {
			segments = append(segments, "ch:"+strings.Join(chanNames, ", "))
		}
		return fmt.Sprintf("go %s(%s)", o.Callee.Name, strings.Join(segments, "; "))
	default:
		return fmt.Sprintf("<unknown op %T>", op)
	}
}

func binOpSymbol(op BinOp) string {
	switch op {
	case Add:
		return "+"
	case Sub:
		return "-"
	case Mul:
		return "*"
	case And:
		return "&"
	case Or:
		return "|"
	case Xor:
		return "^"
	default:
		return "?"
	}
}

func portDirection(dir PortDirection) string {
	switch dir {
	case Input:
		return "in "
	case Output:
		return "out"
	case InOut:
		return "io "
	default:
		return "?"
	}
}

func sensitivity(s Sensitivity) string {
	if s == Sequential {
		return "sequential"
	}
	return "combinational"
}

func signalKind(k SignalKind) string {
	switch k {
	case Wire:
		return "wire"
	case Reg:
		return "reg"
	case Const:
		return "const"
	default:
		return "?"
	}
}

func signSuffix(signed bool) string {
	if signed {
		return "s"
	}
	return "u"
}
