package ir

import (
	"fmt"
	"go/token"
)

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
	Channels  map[string]*Channel
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

// Channel models a FIFO-style buffered channel between processes.
type Channel struct {
	Name      string
	Type      *SignalType
	Depth     int
	Occupancy int
	Source    token.Pos
	Producers []*ChannelEndpoint
	Consumers []*ChannelEndpoint
}

// ChannelEndpoint records how a process interacts with a channel.
type ChannelEndpoint struct {
	Process   *Process
	Direction ChannelDirection
}

// ChannelDirection distinguishes send vs. receive endpoints.
type ChannelDirection int

const (
	ChannelSend ChannelDirection = iota
	ChannelReceive
)

// SignalType records width/sign metadata for a signal.
type SignalType struct {
	Width  int
	Signed bool
}

// Clone returns a deep copy of the signal type.
func (t *SignalType) Clone() *SignalType {
	if t == nil {
		return nil
	}
	clone := *t
	return &clone
}

// IsUnknown reports whether the type lacks width information.
func (t *SignalType) IsUnknown() bool {
	return t == nil || t.Width == 0
}

// Equal reports whether width and signedness match.
func (t *SignalType) Equal(other *SignalType) bool {
	if t == nil && other == nil {
		return true
	}
	if t == nil || other == nil {
		return false
	}
	return t.Width == other.Width && t.Signed == other.Signed
}

// Promote returns a new SignalType that can represent values from both types.
func (t *SignalType) Promote(other *SignalType) *SignalType {
	if t == nil {
		return other.Clone()
	}
	if other == nil {
		return t.Clone()
	}
	result := &SignalType{
		Width:  maxInt(t.Width, other.Width),
		Signed: t.Signed || other.Signed,
	}
	return result
}

// ResultFor returns the output type for applying op to the receiver and other.
func (t *SignalType) ResultFor(op BinOp, other *SignalType) *SignalType {
	switch op {
	case Shl, ShrU, ShrS:
		if t != nil {
			return t.Clone()
		}
		return other.Clone()
	}
	if t == nil {
		return other.Clone()
	}
	result := t.Promote(other)
	if result == nil {
		return nil
	}
	switch op {
	case Mul:
		if t != nil && other != nil {
			result.Width = maxInt(t.Width, other.Width)
		}
	}
	return result
}

// FitsWithin reports whether the receiver fits within the target type without truncation.
func (t *SignalType) FitsWithin(target *SignalType) bool {
	if t == nil || target == nil {
		return true
	}
	if t.IsUnknown() || target.IsUnknown() {
		return true
	}
	return t.Width <= target.Width
}

// SignedCompatible reports whether assigning the receiver into target preserves signedness.
func (t *SignalType) SignedCompatible(target *SignalType) bool {
	if t == nil || target == nil {
		return true
	}
	if t.IsUnknown() || target.IsUnknown() {
		return true
	}
	return t.Signed == target.Signed
}

// Description returns a user-friendly textual representation.
func (t *SignalType) Description() string {
	if t == nil || t.IsUnknown() {
		return "<unknown>"
	}
	sign := "u"
	if t.Signed {
		sign = "s"
	}
	return fmt.Sprintf("%db%s", t.Width, sign)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// AddEndpoint attaches metadata describing how the channel is used.
func (c *Channel) AddEndpoint(proc *Process, dir ChannelDirection) {
	if c == nil || proc == nil {
		return
	}
	endpoint := &ChannelEndpoint{
		Process:   proc,
		Direction: dir,
	}
	switch dir {
	case ChannelSend:
		c.Producers = append(c.Producers, endpoint)
	case ChannelReceive:
		c.Consumers = append(c.Consumers, endpoint)
	}
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
	Name        string
	Sensitivity Sensitivity
	Blocks      []*BasicBlock
	Stage       int
}

// Sensitivity indicates whether process is combinational or sequential.
type Sensitivity int

const (
	Combinational Sensitivity = iota
	Sequential
)

// BasicBlock mirrors SSA basic blocks at the IR level.
type BasicBlock struct {
	Label        string
	Ops          []Operation
	Terminator   Terminator
	Predecessors []*BasicBlock
	Successors   []*BasicBlock
}

// Operation is implemented by every IR operation node.
type Operation interface {
	isOperation()
}

// Terminator ends a basic block and selects the next block.
type Terminator interface {
	isTerminator()
}

// BranchTerminator transfers control based on Cond.
type BranchTerminator struct {
	Cond  *Signal
	True  *BasicBlock
	False *BasicBlock
}

func (BranchTerminator) isTerminator() {}

// JumpTerminator is an unconditional branch to Target.
type JumpTerminator struct {
	Target *BasicBlock
}

func (JumpTerminator) isTerminator() {}

// ReturnTerminator marks block exit from the function.
type ReturnTerminator struct{}

func (ReturnTerminator) isTerminator() {}

// BinOperation models a binary arithmetic operation.
type BinOperation struct {
	Op    BinOp
	Dest  *Signal
	Left  *Signal
	Right *Signal
}

func (BinOperation) isOperation() {}

// CompareOperation performs relational comparison producing a predicate bit.
type CompareOperation struct {
	Predicate ComparePredicate
	Dest      *Signal
	Left      *Signal
	Right     *Signal
}

func (CompareOperation) isOperation() {}

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

// NotOperation performs logical inversion.
type NotOperation struct {
	Dest  *Signal
	Value *Signal
}

func (NotOperation) isOperation() {}

// MuxOperation selects between two values using Cond.
type MuxOperation struct {
	Dest       *Signal
	Cond       *Signal
	TrueValue  *Signal
	FalseValue *Signal
}

func (MuxOperation) isOperation() {}

// PhiOperation captures generic SSA phi merges when not lowered to a mux yet.
type PhiOperation struct {
	Dest      *Signal
	Incomings []PhiIncoming
}

// PhiIncoming ties a predecessor block to the value provided.
type PhiIncoming struct {
	Block *BasicBlock
	Value *Signal
}

func (PhiOperation) isOperation() {}

// PrintVerb enumerates supported formatting styles for print operations.
type PrintVerb int

const (
	PrintVerbDec PrintVerb = iota
	PrintVerbHex
	PrintVerbBin
)

// PrintSegment represents either a literal chunk or a formatted value.
type PrintSegment struct {
	Text  string
	Value *Signal
	Verb  PrintVerb
}

// PrintOperation emits formatted text to the simulator console.
type PrintOperation struct {
	Segments []PrintSegment
}

func (PrintOperation) isOperation() {}

// SendOperation emits a value onto a channel.
type SendOperation struct {
	Channel *Channel
	Value   *Signal
}

func (SendOperation) isOperation() {}

// RecvOperation reads from a channel into Dest.
type RecvOperation struct {
	Channel *Channel
	Dest    *Signal
}

func (RecvOperation) isOperation() {}

// SpawnOperation represents a goroutine launch.
type SpawnOperation struct {
	Callee   *Process
	Args     []*Signal
	ChanArgs []*Channel
}

func (SpawnOperation) isOperation() {}

// BinOp enumerates supported binary ops.
type BinOp int

const (
	Add BinOp = iota
	Sub
	Mul
	And
	Or
	Xor
	Shl
	ShrU
	ShrS
)

// ComparePredicate enumerates supported relational tests.
type ComparePredicate int

const (
	CompareEQ ComparePredicate = iota
	CompareNE
	CompareSLT
	CompareSLE
	CompareSGT
	CompareSGE
	CompareULT
	CompareULE
	CompareUGT
	CompareUGE
)
