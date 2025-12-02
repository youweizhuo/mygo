package mlir

import (
	"fmt"
	"io"
	"math/bits"
	"os"
	"sort"
	"strconv"
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
	decls := portDecls(module.Ports)
	for i, decl := range decls {
		if i > 0 {
			fmt.Fprint(e.w, ", ")
		}
		fmt.Fprint(e.w, decl)
	}
	fmt.Fprint(e.w, ")")
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
		ports := []struct {
			name  string
			value string
			typ   string
		}{
			{name: "clk", value: "%clk", typ: "i1"},
			{name: "rst", value: "%rst", typ: "i1"},
			{name: "in_data", value: wireSet.writeData, typ: elemInout},
			{name: "in_valid", value: wireSet.writeValid, typ: "!hw.inout<i1>"},
			{name: "in_ready", value: wireSet.writeReady, typ: "!hw.inout<i1>"},
			{name: "out_data", value: wireSet.readData, typ: elemInout},
			{name: "out_valid", value: wireSet.readValid, typ: "!hw.inout<i1>"},
			{name: "out_ready", value: wireSet.readReady, typ: "!hw.inout<i1>"},
		}
		for i, port := range ports {
			if i > 0 {
				fmt.Fprint(e.w, ", ")
			}
			fmt.Fprintf(e.w, "%s: %s : %s", port.name, port.value, port.typ)
		}
		fmt.Fprintln(e.w, ") -> ()")
	}
}

func (e *emitter) emitProcessInstance(idx int, info *processInfo, wires map[*ir.Channel]*channelWireSet) {
	if info == nil {
		return
	}
	ports := e.processPorts(info)
	connections := map[string]string{
		"%clk": "%clk",
		"%rst": "%rst",
	}
	for _, ch := range info.channelOrder {
		role := info.channelRoles[ch]
		wire := wires[ch]
		if role == nil || wire == nil {
			continue
		}
		portSet := info.channelPorts[ch]
		if portSet == nil {
			continue
		}
		if role.send {
			connections[portSet.sendData] = wire.writeData
			connections[portSet.sendValid] = wire.writeValid
			connections[portSet.sendReady] = wire.writeReady
		}
		if role.recv {
			connections[portSet.recvData] = wire.readData
			connections[portSet.recvValid] = wire.readValid
			connections[portSet.recvReady] = wire.readReady
		}
	}
	instName := fmt.Sprintf("%s_inst%d", sanitize(info.proc.Name), idx)
	e.printIndent()
	fmt.Fprintf(e.w, "hw.instance \"%s\" @%s(", instName, info.moduleName)
	for i, port := range ports {
		if i > 0 {
			fmt.Fprint(e.w, ", ")
		}
		value := connections[port.name]
		if value == "" {
			value = port.name
		}
		portLabel := strings.TrimPrefix(port.name, "%")
		valueType := port.typ
		if port.inout {
			valueType = fmt.Sprintf("!hw.inout<%s>", port.typ)
		}
		fmt.Fprintf(e.w, "%s: %s : %s", portLabel, value, valueType)
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
		dir := "in"
		if port.inout {
			dir = "inout"
		}
		fmt.Fprintf(e.w, "%s %s: %s", dir, port.name, port.typ)
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
				portDesc{name: portSet.sendData, typ: typeString(ch.Type), inout: true},
				portDesc{name: portSet.sendValid, typ: "i1", inout: true},
				portDesc{name: portSet.sendReady, typ: "i1", inout: true},
			)
		}
		if role.recv {
			portSet.recvData = fmt.Sprintf("%%chan_%s_rdata", sanitize(ch.Name))
			portSet.recvValid = fmt.Sprintf("%%chan_%s_rvalid", sanitize(ch.Name))
			portSet.recvReady = fmt.Sprintf("%%chan_%s_rready", sanitize(ch.Name))
			ports = append(ports,
				portDesc{name: portSet.recvData, typ: typeString(ch.Type), inout: true},
				portDesc{name: portSet.recvValid, typ: "i1", inout: true},
				portDesc{name: portSet.recvReady, typ: "i1", inout: true},
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
	name  string
	typ   string
	inout bool
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
			case *ir.PrintOperation:
				for _, seg := range o.Segments {
					add(seg.Value)
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

type edgeKey struct {
	pred *ir.BasicBlock
	succ *ir.BasicBlock
}

type phiUpdate struct {
	phi   *ir.PhiOperation
	value *ir.Signal
}

type phiRegInfo struct {
	phi       *ir.PhiOperation
	regName   string
	valueName string
	typeStr   string
}

type fsmBuilder struct {
	printer       *processPrinter
	proc          *ir.Process
	blockOrder    []*ir.BasicBlock
	blockIDs      map[*ir.BasicBlock]int
	doneID        int
	stateWidth    int
	stateType     string
	stateConsts   map[int]string
	stateRegInout string
	stateValue    string
	phiInfos      map[*ir.PhiOperation]*phiRegInfo
	phiOrder      []*ir.PhiOperation
	phiUpdates    map[edgeKey][]phiUpdate
}

func newFSMBuilder(printer *processPrinter, proc *ir.Process) *fsmBuilder {
	if printer == nil || proc == nil {
		return nil
	}
	builder := &fsmBuilder{
		printer:     printer,
		proc:        proc,
		blockIDs:    make(map[*ir.BasicBlock]int),
		stateConsts: make(map[int]string),
		phiInfos:    make(map[*ir.PhiOperation]*phiRegInfo),
		phiUpdates:  make(map[edgeKey][]phiUpdate),
	}
	for _, block := range proc.Blocks {
		if block == nil {
			continue
		}
		builder.blockOrder = append(builder.blockOrder, block)
		builder.blockIDs[block] = len(builder.blockOrder) - 1
	}
	builder.doneID = len(builder.blockOrder)
	stateCount := builder.doneID + 1
	if stateCount <= 0 {
		stateCount = 1
	}
	builder.stateWidth = bitWidth(stateCount)
	if builder.stateWidth <= 0 {
		builder.stateWidth = 1
	}
	builder.stateType = fmt.Sprintf("i%d", builder.stateWidth)
	return builder
}

func bitWidth(count int) int {
	if count <= 1 {
		return 1
	}
	return bits.Len(uint(count - 1))
}

func (f *fsmBuilder) emitStateConstants() {
	if f == nil {
		return
	}
	for _, block := range f.blockOrder {
		f.ensureStateConst(f.blockIDs[block])
	}
	f.ensureStateConst(f.doneID)
}

func (f *fsmBuilder) ensureStateConst(id int) string {
	if name, ok := f.stateConsts[id]; ok {
		return name
	}
	if f.printer == nil {
		return ""
	}
	name := f.printer.freshValueName("state_const")
	f.printer.printIndent()
	fmt.Fprintf(f.printer.w, "%s = hw.constant %d : %s\n", name, id, f.stateType)
	f.stateConsts[id] = name
	return name
}

func (f *fsmBuilder) literalForID(id int) string {
	if f.stateWidth <= 0 {
		return fmt.Sprintf("b%d", id)
	}
	return fmt.Sprintf("b%0*b", f.stateWidth, id)
}

func (f *fsmBuilder) emitStateRegister() {
	if f == nil || len(f.blockOrder) == 0 || f.printer == nil {
		return
	}
	entryConst := f.ensureStateConst(0)
	f.stateRegInout = f.printer.freshValueName("state_reg")
	f.printer.printIndent()
	fmt.Fprintf(f.printer.w, "%s = sv.reg : !hw.inout<%s>\n", f.stateRegInout, f.stateType)
	if entryConst != "" {
		f.printer.printIndent()
		fmt.Fprintln(f.printer.w, "sv.initial {")
		f.printer.indent++
		f.printer.printIndent()
		fmt.Fprintf(f.printer.w, "sv.bpassign %s, %s : %s\n", f.stateRegInout, entryConst, f.stateType)
		f.printer.indent--
		f.printer.printIndent()
		fmt.Fprintln(f.printer.w, "}")
	}
	f.stateValue = f.printer.freshValueName("state")
	f.printer.printIndent()
	fmt.Fprintf(f.printer.w, "%s = sv.read_inout %s : !hw.inout<%s>\n", f.stateValue, f.stateRegInout, f.stateType)
}

func (f *fsmBuilder) registerPhi(block *ir.BasicBlock, phi *ir.PhiOperation) {
	if f == nil || f.printer == nil || block == nil || phi == nil || phi.Dest == nil {
		return
	}
	if _, exists := f.phiInfos[phi]; exists {
		return
	}
	typeStr := typeString(phi.Dest.Type)
	regName := f.printer.freshValueName("phi_reg")
	f.printer.printIndent()
	fmt.Fprintf(f.printer.w, "%s = sv.reg : !hw.inout<%s>\n", regName, typeStr)
	destName := f.printer.bindSSA(phi.Dest)
	f.printer.printIndent()
	fmt.Fprintf(f.printer.w, "%s = sv.read_inout %s : !hw.inout<%s>\n", destName, regName, typeStr)
	info := &phiRegInfo{
		phi:       phi,
		regName:   regName,
		valueName: destName,
		typeStr:   typeStr,
	}
	f.phiInfos[phi] = info
	f.phiOrder = append(f.phiOrder, phi)
	for _, incoming := range phi.Incomings {
		if incoming.Block == nil || incoming.Value == nil {
			continue
		}
		key := edgeKey{pred: incoming.Block, succ: block}
		f.phiUpdates[key] = append(f.phiUpdates[key], phiUpdate{
			phi:   phi,
			value: incoming.Value,
		})
	}
}

func (f *fsmBuilder) emitControlLogic() {
	if f == nil || f.printer == nil || f.stateRegInout == "" || f.stateValue == "" {
		return
	}
	clk := f.printer.portRef("clk")
	f.printer.printIndent()
	fmt.Fprintf(f.printer.w, "sv.always posedge %s {\n", clk)
	f.printer.indent++
	if len(f.blockOrder) > 0 {
		f.printer.printIndent()
		fmt.Fprintf(f.printer.w, "sv.case %s : %s\n", f.stateValue, f.stateType)
		for _, block := range f.blockOrder {
			id := f.blockIDs[block]
			f.printer.printIndent()
			fmt.Fprintf(f.printer.w, "case %s: {\n", f.literalForID(id))
			f.printer.indent++
			f.emitBlockCase(block)
			f.printer.indent--
			f.printer.printIndent()
			fmt.Fprintln(f.printer.w, "}")
		}
		f.printer.printIndent()
		fmt.Fprintf(f.printer.w, "case %s: {\n", f.literalForID(f.doneID))
		f.printer.indent++
		f.printer.printIndent()
		fmt.Fprintf(f.printer.w, "sv.passign %s, %s : %s\n", f.stateRegInout, f.stateValue, f.stateType)
		f.printer.indent--
		f.printer.printIndent()
		fmt.Fprintln(f.printer.w, "}")
		f.printer.printIndent()
		fmt.Fprintln(f.printer.w, "default: {")
		f.printer.indent++
		f.printer.printIndent()
		fmt.Fprintf(f.printer.w, "sv.passign %s, %s : %s\n", f.stateRegInout, f.stateValue, f.stateType)
		f.printer.indent--
		f.printer.printIndent()
		fmt.Fprintln(f.printer.w, "}")
	}
	f.printer.indent--
	f.printer.printIndent()
	fmt.Fprintln(f.printer.w, "}")
}

func (f *fsmBuilder) emitBlockCase(block *ir.BasicBlock) {
	if block == nil {
		return
	}
	switch term := block.Terminator.(type) {
	case *ir.BranchTerminator:
		cond := f.printer.valueRef(term.Cond)
		if cond == "%unknown" || cond == "" {
			cond = f.printer.boolConst(false)
		}
		f.printer.printIndent()
		fmt.Fprintf(f.printer.w, "sv.if %s {\n", cond)
		f.printer.indent++
		f.emitTransition(block, term.True)
		f.printer.indent--
		f.printer.printIndent()
		fmt.Fprintln(f.printer.w, "} else {")
		f.printer.indent++
		f.emitTransition(block, term.False)
		f.printer.indent--
		f.printer.printIndent()
		fmt.Fprintln(f.printer.w, "}")
	case *ir.JumpTerminator:
		f.emitTransition(block, term.Target)
	case *ir.ReturnTerminator:
		f.emitTransition(block, nil)
	default:
		// No explicit control transfer; hold previous assignments.
	}
}

func (f *fsmBuilder) emitTransition(pred, succ *ir.BasicBlock) {
	if f == nil || f.printer == nil {
		return
	}
	targetID := f.doneID
	if succ != nil {
		if id, ok := f.blockIDs[succ]; ok {
			targetID = id
		}
	}
	targetConst := f.ensureStateConst(targetID)
	f.printer.printIndent()
	fmt.Fprintf(f.printer.w, "sv.passign %s, %s : %s\n", f.stateRegInout, targetConst, f.stateType)
	if succ == nil {
		return
	}
	key := edgeKey{pred: pred, succ: succ}
	for _, update := range f.phiUpdates[key] {
		info := f.phiInfos[update.phi]
		if info == nil || update.value == nil {
			continue
		}
		val := f.printer.valueRef(update.value)
		if val == "" || val == "%unknown" {
			continue
		}
		f.printer.printIndent()
		fmt.Fprintf(f.printer.w, "sv.passign %s, %s : %s\n", info.regName, val, info.typeStr)
	}
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
	stdoutFD      string
	fsm           *fsmBuilder
	seqClockName  string
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
	p.stdoutFD = ""
	p.fsm = nil
	p.seqClockName = ""
}

func (p *processPrinter) emitProcess(proc *ir.Process) {
	if proc == nil {
		return
	}
	p.emitConstants()
	if processHasPhi(proc) {
		p.fsm = newFSMBuilder(p, proc)
		if p.fsm != nil {
			p.fsm.emitStateConstants()
			p.fsm.emitStateRegister()
		}
	} else {
		p.fsm = nil
	}
	for _, block := range proc.Blocks {
		for _, op := range block.Ops {
			p.emitOperation(block, op, proc)
		}
	}
	if p.fsm != nil {
		p.fsm.emitControlLogic()
	}
	p.fsm = nil
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

func (p *processPrinter) emitOperation(block *ir.BasicBlock, op ir.Operation, proc *ir.Process) {
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
		p.emitConvertOperation(o)
	case *ir.AssignOperation:
		clk := p.seqClock()
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
		fmt.Fprintf(p.w, "sv.assign %s, %s : %s\n",
			ports.sendData,
			value,
			typeString(o.Value.Type),
		)
		validConst := p.boolConst(true)
		p.printIndent()
		fmt.Fprintf(p.w, "sv.assign %s, %s : i1\n",
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
		fmt.Fprintf(p.w, "sv.assign %s, %s : i1\n",
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
		fmt.Fprintf(p.w, "%s = comb.icmp %s %s, %s : %s\n",
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
		fmt.Fprintf(p.w, "%s = comb.mux %s, %s, %s : %s\n",
			dest,
			cond,
			tVal,
			fVal,
			typeString(o.Dest.Type),
		)
	case *ir.PhiOperation:
		if p.fsm != nil {
			p.fsm.registerPhi(block, o)
		} else {
			p.printIndent()
			fmt.Fprintf(p.w, "// phi %s has %d incoming values\n", sanitize(o.Dest.Name), len(o.Incomings))
		}
	case *ir.PrintOperation:
		p.emitPrintOperation(o)
	default:
		// skip unknown operations
	}
}

func (p *processPrinter) seqClock() string {
	if p.seqClockName != "" {
		return p.seqClockName
	}
	clk := p.portRef("clk")
	name := p.freshValueName("clk_seq")
	p.printIndent()
	fmt.Fprintf(p.w, "%s = seq.to_clock %s\n", name, clk)
	p.seqClockName = name
	return name
}

func processHasPhi(proc *ir.Process) bool {
	if proc == nil {
		return false
	}
	for _, block := range proc.Blocks {
		for _, op := range block.Ops {
			if _, ok := op.(*ir.PhiOperation); ok {
				return true
			}
		}
	}
	return false
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
	fmt.Fprintf(p.w, "%s = hw.constant %t\n", name, val)
	return name
}

func (p *processPrinter) freshValueName(prefix string) string {
	if prefix == "" {
		prefix = "tmp"
	}
	name := fmt.Sprintf("%%%s%d", prefix, p.nextTemp)
	p.nextTemp++
	return name
}

func (p *processPrinter) emitConvertOperation(o *ir.ConvertOperation) {
	if o == nil || o.Value == nil || o.Dest == nil {
		return
	}
	srcWidth := signalWidth(o.Value.Type)
	destWidth := signalWidth(o.Dest.Type)
	src := p.valueRef(o.Value)
	dest := p.bindSSA(o.Dest)
	from := typeString(o.Value.Type)
	to := typeString(o.Dest.Type)

	switch {
	case destWidth == srcWidth:
		p.printIndent()
		fmt.Fprintf(p.w, "%s = comb.bitcast %s : %s -> %s\n", dest, src, from, to)
	case destWidth > srcWidth:
		extendWidth := destWidth - srcWidth
		if extendWidth <= 0 {
			p.printIndent()
			fmt.Fprintf(p.w, "%s = comb.bitcast %s : %s -> %s\n", dest, src, from, to)
			return
		}
		if o.Value.Type != nil && o.Value.Type.Signed {
			signBit := p.freshValueName("sext_msb")
			p.printIndent()
			fmt.Fprintf(p.w, "%s = comb.extract %s from %d : (%s) -> i1\n",
				signBit,
				src,
				srcWidth-1,
				from,
			)
			replicated := p.freshValueName("sext_bits")
			p.printIndent()
			fmt.Fprintf(p.w, "%s = comb.replicate %s : (i1) -> i%d\n",
				replicated,
				signBit,
				extendWidth,
			)
			p.printIndent()
			fmt.Fprintf(p.w, "%s = comb.concat %s, %s : i%d, %s\n",
				dest,
				replicated,
				src,
				extendWidth,
				from,
			)
		} else {
			p.printIndent()
			zeros := p.freshValueName("zext_pad")
			fmt.Fprintf(p.w, "%s = hw.constant 0 : i%d\n", zeros, extendWidth)
			p.printIndent()
			fmt.Fprintf(p.w, "%s = comb.concat %s, %s : i%d, %s\n",
				dest,
				zeros,
				src,
				extendWidth,
				from,
			)
		}
	default:
		p.printIndent()
		fmt.Fprintf(p.w, "%s = comb.extract %s from 0 : (%s) -> %s\n",
			dest,
			src,
			from,
			to,
		)
	}
}

func (p *processPrinter) emitPrintOperation(op *ir.PrintOperation) {
	if op == nil {
		return
	}
	format, operands, operandTypes := p.buildPrintfFormat(op)
	fd := p.stdoutConstant()
	clk := p.portRef("clk")

	p.printIndent()
	fmt.Fprintf(p.w, "sv.always posedge %s {\n", clk)
	p.indent++
	p.printIndent()
	if len(operands) == 0 {
		fmt.Fprintf(p.w, "sv.fwrite %s, %s\n", fd, strconv.Quote(format))
	} else {
		fmt.Fprintf(p.w, "sv.fwrite %s, %s(%s) : %s\n",
			fd,
			strconv.Quote(format),
			strings.Join(operands, ", "),
			strings.Join(operandTypes, ", "),
		)
	}
	p.indent--
	p.printIndent()
	fmt.Fprintln(p.w, "}")
}

func (p *processPrinter) buildPrintfFormat(op *ir.PrintOperation) (string, []string, []string) {
	var builder strings.Builder
	var values []string
	var types []string

	for _, seg := range op.Segments {
		if seg.Value == nil {
			builder.WriteString(escapePercent(seg.Text))
			continue
		}
		values = append(values, p.valueRef(seg.Value))
		types = append(types, typeString(seg.Value.Type))
		builder.WriteString(printVerbSpecifier(seg.Verb))
	}
	return builder.String(), values, types
}

func escapePercent(text string) string {
	return strings.ReplaceAll(text, "%", "%%")
}

func printVerbSpecifier(verb ir.PrintVerb) string {
	switch verb {
	case ir.PrintVerbHex:
		return "%x"
	case ir.PrintVerbBin:
		return "%b"
	default:
		return "%d"
	}
}

func (p *processPrinter) stdoutConstant() string {
	if p.stdoutFD != "" {
		return p.stdoutFD
	}
	name := p.freshValueName("stdout_fd")
	p.printIndent()
	fmt.Fprintf(p.w, "%s = hw.constant %d : i32\n", name, 0x80000001)
	p.stdoutFD = name
	return name
}

func portDecls(ports []ir.Port) []string {
	decls := make([]string, 0, len(ports))
	for _, port := range ports {
		switch port.Direction {
		case ir.Output:
			decls = append(decls, fmt.Sprintf("out %s: %s", sanitize(port.Name), typeString(port.Type)))
		default:
			decls = append(decls, fmt.Sprintf("in %%%s: %s", sanitize(port.Name), typeString(port.Type)))
		}
	}
	return decls
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
	case ir.Shl:
		return "shl"
	case ir.ShrU:
		return "shru"
	case ir.ShrS:
		return "shrs"
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
		elemType := typeString(info.elemType)
		e.printIndent()
		fmt.Fprintf(e.w, "hw.module @%s(in %%clk: i1, in %%rst: i1, inout %%in_data: %s, inout %%in_valid: i1, inout %%in_ready: i1, inout %%out_data: %s, inout %%out_valid: i1, inout %%out_ready: i1) {\n",
			info.moduleName,
			elemType,
			elemType,
		)
		e.indent++
		e.printIndent()
		fmt.Fprintln(e.w, "hw.output")
		e.indent--
		e.printIndent()
		fmt.Fprintln(e.w, "}")
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

func signalWidth(t *ir.SignalType) int {
	if t == nil || t.Width <= 0 {
		return 1
	}
	return t.Width
}
