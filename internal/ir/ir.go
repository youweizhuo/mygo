package ir

import "go/token"

// Design is the top-level hardware description consisting of one or more modules.
type Design struct {
	Modules  []*Module
	TopLevel *Module
}

// Module models a hardware module with ports, signals and processes.
type Module struct {
	Name      string
	Ports     []Port
	Signals   map[string]*Signal
	Processes []*Process
	Source    token.Pos
}

// Port represents a module IO port.
type Port struct {
	Name      string
	Direction PortDirection
	Type      *SignalType
}

// PortDirection enumerates supported port directions.
type PortDirection int

const (
	Input PortDirection = iota
	Output
	InOut
)

// Signal captures a hardware wire/register.
type Signal struct {
	Name   string
	Type   *SignalType
	Kind   SignalKind
	Value  interface{}
	Source token.Pos
}

// SignalType records width/sign metadata for a signal.
type SignalType struct {
	Width  int
	Signed bool
}

// SignalKind classifies how a signal is driven.
type SignalKind int

const (
	Wire SignalKind = iota
	Reg
	Const
)

// Process groups a sequence of operations under a specific clocking scheme.
type Process struct {
	Sensitivity Sensitivity
	Blocks      []*BasicBlock
}

// Sensitivity indicates whether process is combinational or sequential.
type Sensitivity int

const (
	Combinational Sensitivity = iota
	Sequential
)

// BasicBlock mirrors SSA basic blocks at the IR level.
type BasicBlock struct {
	Label string
	Ops   []Operation
}

// Operation is implemented by every IR operation node.
type Operation interface {
	isOperation()
}

// BinOperation models a binary arithmetic operation.
type BinOperation struct {
	Op    BinOp
	Dest  *Signal
	Left  *Signal
	Right *Signal
}

func (BinOperation) isOperation() {}

// AssignOperation copies one signal to another (e.g. store).
type AssignOperation struct {
	Dest  *Signal
	Value *Signal
}

func (AssignOperation) isOperation() {}

// ConvertOperation represents a type conversion (zero/sign extension etc.).
type ConvertOperation struct {
	Dest  *Signal
	Value *Signal
}

func (ConvertOperation) isOperation() {}

// BinOp enumerates supported binary ops.
type BinOp int

const (
	Add BinOp = iota
	Sub
	Mul
	And
	Or
	Xor
)
