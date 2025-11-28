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

	em := &emitter{
		w:         w,
		fifoDecls: make(map[string]*fifoInfo),
	}
	fmt.Fprintln(w, "module {")
	em.indent++
	for _, module := range design.Modules {
		em.emitModule(module)
	}
	em.emitFifoExterns()
	em.indent--
	fmt.Fprintln(w, "}")
	return nil
}

type emitter struct {
	w         io.Writer
	indent    int
	fifoDecls map[string]*fifoInfo
}

func (e *emitter) emitModule(module *ir.Module) {
	if module == nil {
		return
	}
	processInfos := buildProcessInfos(module)
	var root *processInfo
	others := make([]*processInfo, 0, len(processInfos))
	for _, info := range processInfos {
		if info.proc != nil && info.proc.Name == module.Name && root == nil {
			root = info
			continue
		}
		others = append(others, info)
	}
	e.emitTopLevelModule(module, root, others)
	for _, info := range others {
		e.emitProcessModule(module, info)
	}
}

func (e *emitter) emitTopLevelModule(module *ir.Module, root *processInfo, processes []*processInfo) map[*ir.Channel]*channelWireSet {
	e.printIndent()
	fmt.Fprintf(e.w, "hw.module @%s(", module.Name)
	inputs, outputs := portLists(module.Ports)
	for i, in := range inputs {
		if i > 0 {
			fmt.Fprint(e.w, ", ")
		}
		fmt.Fprint(e.w, in)
	}
	fmt.Fprint(e.w, ")")
	if len(outputs) > 0 {
		fmt.Fprint(e.w, " -> (")
		for i, out := range outputs {
			if i > 0 {
				fmt.Fprint(e.w, ", ")
			}
			fmt.Fprint(e.w, out)
		}
		fmt.Fprint(e.w, ")")
	}
	fmt.Fprintln(e.w, " {")
	e.indent++

	channelWires := e.emitChannelWires(module)
	e.emitChannelFifos(module, channelWires)
	if root != nil {
		e.emitRootProcess(module, root, channelWires)
	}
	for idx, info := range processes {
		e.emitProcessInstance(idx, info, channelWires)
	}

	e.printIndent()
	fmt.Fprintln(e.w, "hw.output")
	e.indent--
	e.printIndent()
	fmt.Fprintln(e.w, "}")
	return channelWires
}

func (e *emitter) emitChannelWires(module *ir.Module) map[*ir.Channel]*channelWireSet {
	wires := make(map[*ir.Channel]*channelWireSet)
	if module == nil {
		return wires
	}
	names := make([]string, 0, len(module.Channels))
	for name := range module.Channels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ch := module.Channels[name]
		s := sanitize(ch.Name)
		wireSet := &channelWireSet{
			writeData:  fmt.Sprintf("%%chan_%s_wdata", s),
			writeValid: fmt.Sprintf("%%chan_%s_wvalid", s),
			writeReady: fmt.Sprintf("%%chan_%s_wready", s),
			readData:   fmt.Sprintf("%%chan_%s_rdata", s),
			readValid:  fmt.Sprintf("%%chan_%s_rvalid", s),
			readReady:  fmt.Sprintf("%%chan_%s_rready", s),
		}
		wires[ch] = wireSet
		e.printIndent()
		fmt.Fprintf(e.w, "// channel %s depth=%d type=%s\n", ch.Name, ch.Depth, typeString(ch.Type))
		e.printIndent()
		fmt.Fprintf(e.w, "%s = sv.wire : %s\n", wireSet.writeData, inoutTypeString(ch.Type))
		e.printIndent()
		fmt.Fprintf(e.w, "%s = sv.wire : !hw.inout<i1>\n", wireSet.writeValid)
		e.printIndent()
		fmt.Fprintf(e.w, "%s = sv.wire : !hw.inout<i1>\n", wireSet.writeReady)
		e.printIndent()
		fmt.Fprintf(e.w, "%s = sv.wire : %s\n", wireSet.readData, inoutTypeString(ch.Type))
		e.printIndent()
		fmt.Fprintf(e.w, "%s = sv.wire : !hw.inout<i1>\n", wireSet.readValid)
		e.printIndent()
		fmt.Fprintf(e.w, "%s = sv.wire : !hw.inout<i1>\n", wireSet.readReady)
		e.emitChannelMetadata(ch)
	}
	return wires
}

func (e *emitter) emitChannelFifos(module *ir.Module, wires map[*ir.Channel]*channelWireSet) {
	if module == nil || len(module.Channels) == 0 {
		return
	}
	names := make([]string, 0, len(module.Channels))
	for name := range module.Channels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ch := module.Channels[name]
		wireSet := wires[ch]
		elemInout := inoutTypeString(ch.Type)
		moduleName := fifoModuleName(ch)
		e.recordFifo(moduleName, ch)
		e.printIndent()
		fmt.Fprintf(e.w, "hw.instance \"%s_fifo\" @%s(", sanitize(ch.Name), moduleName)
		args := []string{
			"%clk",
			"%rst",
			wireSet.writeData,
			wireSet.writeValid,
			wireSet.writeReady,
			wireSet.readData,
			wireSet.readValid,
			wireSet.readReady,
		}
		for i, arg := range args {
			if i > 0 {
				fmt.Fprint(e.w, ", ")
			}
			fmt.Fprint(e.w, arg)
		}
		fmt.Fprintf(e.w, ") : (i1, i1, %s, !hw.inout<i1>, !hw.inout<i1>, %s, !hw.inout<i1>, !hw.inout<i1>) -> ()\n", elemInout, elemInout)
	}
}

func (e *emitter) emitProcessInstance(idx int, info *processInfo, wires map[*ir.Channel]*channelWireSet) {
	if info == nil {
		return
	}
	args := []string{"%clk", "%rst"}
	types := []string{"i1", "i1"}
	for _, ch := range info.channelOrder {
		role := info.channelRoles[ch]
		wire := wires[ch]
		if role == nil || wire == nil {
			continue
		}
		if role.send {
			args = append(args, wire.writeData, wire.writeValid, wire.writeReady)
			types = append(types, inoutTypeString(ch.Type), "!hw.inout<i1>", "!hw.inout<i1>")
		}
		if role.recv {
			args = append(args, wire.readData, wire.readValid, wire.readReady)
			types = append(types, inoutTypeString(ch.Type), "!hw.inout<i1>", "!hw.inout<i1>")
		}
	}
	instName := fmt.Sprintf("%s_inst%d", sanitize(info.proc.Name), idx)
	e.printIndent()
	fmt.Fprintf(e.w, "hw.instance \"%s\" @%s(", instName, info.moduleName)
	for i, arg := range args {
		if i > 0 {
			fmt.Fprint(e.w, ", ")
		}
		fmt.Fprint(e.w, arg)
	}
	fmt.Fprintf(e.w, ") : (")
	for i, typ := range types {
		if i > 0 {
			fmt.Fprint(e.w, ", ")
		}
		fmt.Fprint(e.w, typ)
	}
	fmt.Fprintln(e.w, ") -> ()")
}

func (e *emitter) emitProcessModule(module *ir.Module, info *processInfo) {
	if info == nil || info.proc == nil {
		return
	}
	ports := e.processPorts(info)
	e.printIndent()
	fmt.Fprintf(e.w, "hw.module @%s(", info.moduleName)
	for i, port := range ports {
		if i > 0 {
			fmt.Fprint(e.w, ", ")
		}
		fmt.Fprintf(e.w, "%s: %s", port.name, port.typ)
	}
	fmt.Fprintln(e.w, ") {")
	e.indent++

	pp := &processPrinter{
		w:             e.w,
		indent:        e.indent,
		moduleSignals: module.Signals,
		usedSignals:   info.usedSignals,
		channelPorts:  info.channelPorts,
	}
	pp.resetState()
	pp.emitProcess(info.proc)

	e.indent--
	e.printIndent()
	fmt.Fprintln(e.w, "}")
}

func (e *emitter) emitRootProcess(module *ir.Module, info *processInfo, wires map[*ir.Channel]*channelWireSet) {
	if info == nil || info.proc == nil {
		return
	}
	pp := &processPrinter{
		w:             e.w,
		indent:        e.indent,
		moduleSignals: module.Signals,
		usedSignals:   info.usedSignals,
		channelPorts:  channelPortsFromWires(info, wires),
	}
	pp.resetState()
	pp.emitProcess(info.proc)
}

func (e *emitter) processPorts(info *processInfo) []portDesc {
	ports := []portDesc{
		{name: "%clk", typ: "i1"},
		{name: "%rst", typ: "i1"},
	}
	for _, ch := range info.channelOrder {
		role := info.channelRoles[ch]
		if role == nil {
			continue
		}
		portSet := info.channelPorts[ch]
		if portSet == nil {
			portSet = &channelPortSet{}
			info.channelPorts[ch] = portSet
		}
		if role.send {
			portSet.sendData = fmt.Sprintf("%%chan_%s_wdata", sanitize(ch.Name))
			portSet.sendValid = fmt.Sprintf("%%chan_%s_wvalid", sanitize(ch.Name))
			portSet.sendReady = fmt.Sprintf("%%chan_%s_wready", sanitize(ch.Name))
			ports = append(ports,
				portDesc{name: portSet.sendData, typ: inoutTypeString(ch.Type)},
				portDesc{name: portSet.sendValid, typ: "!hw.inout<i1>"},
				portDesc{name: portSet.sendReady, typ: "!hw.inout<i1>"},
			)
		}
		if role.recv {
			portSet.recvData = fmt.Sprintf("%%chan_%s_rdata", sanitize(ch.Name))
			portSet.recvValid = fmt.Sprintf("%%chan_%s_rvalid", sanitize(ch.Name))
			portSet.recvReady = fmt.Sprintf("%%chan_%s_rready", sanitize(ch.Name))
			ports = append(ports,
				portDesc{name: portSet.recvData, typ: inoutTypeString(ch.Type)},
				portDesc{name: portSet.recvValid, typ: "!hw.inout<i1>"},
				portDesc{name: portSet.recvReady, typ: "!hw.inout<i1>"},
			)
		}
	}
	return ports
}

func (e *emitter) emitChannelMetadata(ch *ir.Channel) {
	if ch == nil {
		return
	}
	e.printIndent()
	fmt.Fprintf(e.w, "// channel %s occupancy %d/%d\n", sanitize(ch.Name), ch.Occupancy, ch.Depth)
	for _, prod := range ch.Producers {
		stage := processStage(prod.Process)
		name := processName(prod.Process)
		e.printIndent()
		fmt.Fprintf(e.w, "//   producer %s stage %d\n", name, stage)
	}
	for _, cons := range ch.Consumers {
		stage := processStage(cons.Process)
		name := processName(cons.Process)
		e.printIndent()
		fmt.Fprintf(e.w, "//   consumer %s stage %d\n", name, stage)
	}
}

func (e *emitter) printIndent() {
	for i := 0; i < e.indent; i++ {
		fmt.Fprint(e.w, "  ")
	}
}

type portDesc struct {
	name string
	typ  string
}

type channelRole struct {
	send bool
	recv bool
}

type channelPortSet struct {
	sendData  string
	sendValid string
	sendReady string
	recvData  string
	recvValid string
	recvReady string
}

type channelWireSet struct {
	writeData  string
	writeValid string
	writeReady string
	readData   string
	readValid  string
	readReady  string
}

type fifoInfo struct {
	moduleName string
	elemType   *ir.SignalType
	depth      int
}

func channelPortsFromWires(info *processInfo, wires map[*ir.Channel]*channelWireSet) map[*ir.Channel]*channelPortSet {
	ports := make(map[*ir.Channel]*channelPortSet)
	if info == nil {
		return ports
	}
	for _, ch := range info.channelOrder {
		role := info.channelRoles[ch]
		wire := wires[ch]
		if role == nil || wire == nil {
			continue
		}
		set := &channelPortSet{}
		if role.send {
			set.sendData = wire.writeData
			set.sendValid = wire.writeValid
			set.sendReady = wire.writeReady
		}
		if role.recv {
			set.recvData = wire.readData
			set.recvValid = wire.readValid
			set.recvReady = wire.readReady
		}
		ports[ch] = set
	}
	return ports
}

type processInfo struct {
	proc         *ir.Process
	moduleName   string
	channelOrder []*ir.Channel
	channelRoles map[*ir.Channel]*channelRole
	channelPorts map[*ir.Channel]*channelPortSet
	usedSignals  map[*ir.Signal]struct{}
}

func buildProcessInfos(module *ir.Module) []*processInfo {
	if module == nil {
		return nil
	}
	infos := make([]*processInfo, 0, len(module.Processes))
	for _, proc := range module.Processes {
		if proc == nil {
			continue
		}
		roles, order := collectProcessChannelRoles(proc)
		info := &processInfo{
			proc:         proc,
			moduleName:   processModuleName(module, proc),
			channelOrder: order,
			channelRoles: roles,
			channelPorts: make(map[*ir.Channel]*channelPortSet),
			usedSignals:  collectProcessSignals(proc),
		}
		infos = append(infos, info)
	}
	sort.SliceStable(infos, func(i, j int) bool {
		return infos[i].moduleName < infos[j].moduleName
	})
	return infos
}

func processModuleName(module *ir.Module, proc *ir.Process) string {
	modName := "module"
	if module != nil && module.Name != "" {
		modName = sanitize(module.Name)
	}
	procName := processName(proc)
	return fmt.Sprintf("%s__proc_%s", modName, procName)
}

func collectProcessChannelRoles(proc *ir.Process) (map[*ir.Channel]*channelRole, []*ir.Channel) {
	roles := make(map[*ir.Channel]*channelRole)
	if proc == nil {
		return roles, nil
	}
	for _, block := range proc.Blocks {
		for _, op := range block.Ops {
			switch o := op.(type) {
			case *ir.SendOperation:
				if o.Channel == nil {
					continue
				}
				role := roles[o.Channel]
				if role == nil {
					role = &channelRole{}
					roles[o.Channel] = role
				}
				role.send = true
			case *ir.RecvOperation:
				if o.Channel == nil {
					continue
				}
				role := roles[o.Channel]
				if role == nil {
					role = &channelRole{}
					roles[o.Channel] = role
				}
				role.recv = true
			}
		}
	}
	order := make([]*ir.Channel, 0, len(roles))
	for ch := range roles {
		order = append(order, ch)
	}
	sort.Slice(order, func(i, j int) bool {
		return sanitize(order[i].Name) < sanitize(order[j].Name)
	})
	return roles, order
}

func collectProcessSignals(proc *ir.Process) map[*ir.Signal]struct{} {
	used := make(map[*ir.Signal]struct{})
	if proc == nil {
		return used
	}
	add := func(sig *ir.Signal) {
		if sig != nil {
			used[sig] = struct{}{}
		}
	}
	for _, block := range proc.Blocks {
		for _, op := range block.Ops {
			switch o := op.(type) {
			case *ir.BinOperation:
				add(o.Left)
				add(o.Right)
				add(o.Dest)
			case *ir.ConvertOperation:
				add(o.Value)
				add(o.Dest)
			case *ir.AssignOperation:
				add(o.Value)
				add(o.Dest)
			case *ir.SendOperation:
				add(o.Value)
			case *ir.RecvOperation:
				add(o.Dest)
			case *ir.CompareOperation:
				add(o.Left)
				add(o.Right)
				add(o.Dest)
			case *ir.NotOperation:
				add(o.Value)
				add(o.Dest)
			case *ir.MuxOperation:
				add(o.Cond)
				add(o.TrueValue)
				add(o.FalseValue)
				add(o.Dest)
			case *ir.PhiOperation:
				add(o.Dest)
				for _, in := range o.Incomings {
					add(in.Value)
				}
			case *ir.SpawnOperation:
				for _, arg := range o.Args {
					add(arg)
				}
			}
		}
		if block.Terminator != nil {
			switch term := block.Terminator.(type) {
			case *ir.BranchTerminator:
				add(term.Cond)
			}
		}
	}
	return used
}

type processPrinter struct {
	w             io.Writer
	indent        int
	nextTemp      int
	constNames    map[*ir.Signal]string
	valueNames    map[*ir.Signal]string
	portNames     map[string]string
	channelPorts  map[*ir.Channel]*channelPortSet
	moduleSignals map[string]*ir.Signal
	usedSignals   map[*ir.Signal]struct{}
	boolConsts    map[bool]string
}

func (p *processPrinter) resetState() {
	p.nextTemp = 0
	p.constNames = make(map[*ir.Signal]string)
	p.valueNames = make(map[*ir.Signal]string)
	p.portNames = map[string]string{
		"clk": "%clk",
		"rst": "%rst",
	}
	if p.channelPorts == nil {
		p.channelPorts = make(map[*ir.Channel]*channelPortSet)
	}
	if p.usedSignals == nil {
		p.usedSignals = make(map[*ir.Signal]struct{})
	}
	if p.boolConsts == nil {
		p.boolConsts = make(map[bool]string)
	}
}

func (p *processPrinter) emitProcess(proc *ir.Process) {
	if proc == nil {
		return
	}
	p.emitConstants()
	for _, block := range proc.Blocks {
		for _, op := range block.Ops {
			p.emitOperation(op, proc)
		}
	}
}

func (p *processPrinter) emitConstants() {
	if len(p.moduleSignals) == 0 {
		return
	}
	names := make([]string, 0, len(p.moduleSignals))
	for name := range p.moduleSignals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sig := p.moduleSignals[name]
		if sig.Kind != ir.Const {
			continue
		}
		if _, ok := p.usedSignals[sig]; !ok {
			continue
		}
		ssaName := p.assignConst(sig)
		p.printIndent()
		fmt.Fprintf(p.w, "%s = hw.constant %v : %s\n", ssaName, sig.Value, typeString(sig.Type))
	}
}

func (p *processPrinter) emitOperation(op ir.Operation, proc *ir.Process) {
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
		p.printIndent()
		fmt.Fprintf(p.w, "%s = seq.compreg %s, %s : %s\n", dest, src, clk, typeString(o.Dest.Type))
	case *ir.SendOperation:
		value := p.valueRef(o.Value)
		ports := p.channelPorts[o.Channel]
		if ports == nil || ports.sendData == "" {
			p.printIndent()
			fmt.Fprintf(p.w, "// missing channel send ports for %s\n", sanitize(o.Channel.Name))
			return
		}
		p.printIndent()
		fmt.Fprintf(p.w, "sv.assign %s, %s : %s, %s\n",
			ports.sendData,
			value,
			inoutTypeString(o.Channel.Type),
			typeString(o.Value.Type),
		)
		validConst := p.boolConst(true)
		p.printIndent()
		fmt.Fprintf(p.w, "sv.assign %s, %s : !hw.inout<i1>, i1\n",
			ports.sendValid,
			validConst,
		)
	case *ir.RecvOperation:
		dest := p.bindSSA(o.Dest)
		ports := p.channelPorts[o.Channel]
		if ports == nil || ports.recvData == "" {
			p.printIndent()
			fmt.Fprintf(p.w, "// missing channel recv ports for %s\n", sanitize(o.Channel.Name))
			return
		}
		p.printIndent()
		fmt.Fprintf(p.w, "%s = sv.read_inout %s : %s\n",
			dest,
			ports.recvData,
			inoutTypeString(o.Channel.Type),
		)
		readyConst := p.boolConst(true)
		p.printIndent()
		fmt.Fprintf(p.w, "sv.assign %s, %s : !hw.inout<i1>, i1\n",
			ports.recvReady,
			readyConst,
		)
	case *ir.SpawnOperation:
		childStage := processStage(o.Callee)
		parentStage := processStage(proc)
		p.printIndent()
		fmt.Fprintf(p.w, "// spawn %s stage=%d parent_stage=%d\n",
			sanitize(o.Callee.Name),
			childStage,
			parentStage,
		)
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
		// skip unknown operations
	}
}

func (p *processPrinter) assignConst(sig *ir.Signal) string {
	if name, ok := p.constNames[sig]; ok {
		return name
	}
	name := fmt.Sprintf("%%c%d", p.nextTemp)
	p.nextTemp++
	p.constNames[sig] = name
	return name
}

func (p *processPrinter) bindSSA(sig *ir.Signal) string {
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

func (p *processPrinter) valueRef(sig *ir.Signal) string {
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

func (p *processPrinter) portRef(name string) string {
	if val, ok := p.portNames[name]; ok {
		return val
	}
	return fmt.Sprintf("%%%s", sanitize(name))
}

func (p *processPrinter) printIndent() {
	for i := 0; i < p.indent; i++ {
		fmt.Fprint(p.w, "  ")
	}
}

func (p *processPrinter) boolConst(val bool) string {
	if name, ok := p.boolConsts[val]; ok {
		return name
	}
	name := fmt.Sprintf("%%c_bool_%d", len(p.boolConsts))
	p.boolConsts[val] = name
	p.printIndent()
	fmt.Fprintf(p.w, "%s = hw.constant %t : i1\n", name, val)
	return name
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

func inoutTypeString(t *ir.SignalType) string {
	return fmt.Sprintf("!hw.inout<%s>", typeString(t))
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

func processStage(proc *ir.Process) int {
	if proc == nil {
		return 0
	}
	if proc.Stage < 0 {
		return 0
	}
	return proc.Stage
}

func processName(proc *ir.Process) string {
	if proc == nil || proc.Name == "" {
		return "unnamed_process"
	}
	return sanitize(proc.Name)
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

func (e *emitter) recordFifo(moduleName string, ch *ir.Channel) {
	if ch == nil {
		return
	}
	if _, ok := e.fifoDecls[moduleName]; ok {
		return
	}
	info := &fifoInfo{
		moduleName: moduleName,
		elemType:   ch.Type,
		depth:      ch.Depth,
	}
	e.fifoDecls[moduleName] = info
}

func (e *emitter) emitFifoExterns() {
	if len(e.fifoDecls) == 0 {
		return
	}
	names := make([]string, 0, len(e.fifoDecls))
	for name := range e.fifoDecls {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		info := e.fifoDecls[name]
		elemInout := inoutTypeString(info.elemType)
		e.printIndent()
		fmt.Fprintf(e.w, "hw.module @%s(%%clk: i1, %%rst: i1, %%in_data: %s, %%in_valid: !hw.inout<i1>, %%in_ready: !hw.inout<i1>, %%out_data: %s, %%out_valid: !hw.inout<i1>, %%out_ready: !hw.inout<i1>) external\n",
			info.moduleName,
			elemInout,
			elemInout,
		)
	}
}

func fifoModuleName(ch *ir.Channel) string {
	if ch == nil {
		return "mygo_fifo_i1_d1"
	}
	depth := ch.Depth
	if depth <= 0 {
		depth = 1
	}
	return fmt.Sprintf("mygo_fifo_%s_d%d", sanitize(typeString(ch.Type)), depth)
}
