package internal

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"math/big"
	"strings"

	"github.com/ebukreev/go-z3/z3"
	"golang.org/x/tools/go/ssa"
	"symbolic-execution-course/internal/memory"
	"symbolic-execution-course/internal/symbolic"
)

type ExecutionStatus string

const (
	Running      ExecutionStatus = ""
	Returned     ExecutionStatus = "returned"
	Panicked     ExecutionStatus = "panicked"
	LimitReached ExecutionStatus = "limit reached"
)

type Interpreter struct {
	CallStack     []CallStackFrame
	Analyser      *Analyser
	PathCondition symbolic.SymbolicExpression
	Heap          memory.Memory
	Status        ExecutionStatus
	Diagnostic    string
	pathFormula   z3.Bool
}

type CallStackFrame struct {
	Function         *ssa.Function
	LocalMemory      map[string]symbolic.SymbolicExpression
	ReturnValue      symbolic.SymbolicExpression
	ReturnValues     []symbolic.SymbolicExpression
	CurrentBlock     *ssa.BasicBlock
	PreviousBlock    *ssa.BasicBlock
	InstructionIndex int
}

func (interpreter *Interpreter) frame() *CallStackFrame {
	return &interpreter.CallStack[len(interpreter.CallStack)-1]
}

func (interpreter *Interpreter) clone() Interpreter {
	result := *interpreter
	result.CallStack = append([]CallStackFrame(nil), interpreter.CallStack...)
	for i := range result.CallStack {
		frame := &result.CallStack[i]
		frame.LocalMemory = make(map[string]symbolic.SymbolicExpression, len(interpreter.CallStack[i].LocalMemory))
		for name, value := range interpreter.CallStack[i].LocalMemory {
			frame.LocalMemory[name] = value
		}
		frame.ReturnValues = append([]symbolic.SymbolicExpression(nil), frame.ReturnValues...)
	}
	return result
}

func (interpreter *Interpreter) enter(block *ssa.BasicBlock) {
	frame := interpreter.frame()
	frame.PreviousBlock, frame.CurrentBlock, frame.InstructionIndex = frame.CurrentBlock, block, 0
	predecessor := -1
	for i, pred := range block.Preds {
		if pred == frame.PreviousBlock {
			predecessor = i
			break
		}
	}
	values := make(map[string]symbolic.SymbolicExpression)
	for _, instruction := range block.Instrs {
		phi, ok := instruction.(*ssa.Phi)
		if !ok {
			break
		}
		if predecessor < 0 {
			panic("analysis: missing phi predecessor")
		}
		values["%"+phi.Name()] = interpreter.resolveExpression(phi.Edges[predecessor])
		frame.InstructionIndex++
	}
	for name, value := range values {
		frame.LocalMemory[name] = value
	}
}

func (interpreter *Interpreter) restrict(condition symbolic.SymbolicExpression) bool {
	if literal, ok := condition.(*symbolic.BoolConstant); ok {
		return literal.Value
	}
	interpreter.PathCondition = symbolic.NewLogicalOperation([]symbolic.SymbolicExpression{interpreter.PathCondition, condition}, symbolic.AND)
	value, err := interpreter.Analyser.Z3Translator.TranslateExpression(condition)
	if err != nil {
		panic(fmt.Sprintf("analysis: translate path: %v", err))
	}
	interpreter.pathFormula = interpreter.pathFormula.And(value.(z3.Bool))
	return interpreter.Analyser.feasible(interpreter.pathFormula)
}

func negate(condition symbolic.SymbolicExpression) symbolic.SymbolicExpression {
	if literal, ok := condition.(*symbolic.BoolConstant); ok {
		return symbolic.NewBoolConstant(!literal.Value)
	}
	return symbolic.NewLogicalOperation([]symbolic.SymbolicExpression{condition}, symbolic.NOT)
}

func (interpreter *Interpreter) interpretDynamically(element ssa.Instruction) []Interpreter {
	switch instruction := element.(type) {
	case *ssa.If:
		condition := interpreter.resolveExpression(instruction.Cond)
		var states []Interpreter
		for i, guard := range []symbolic.SymbolicExpression{condition, negate(condition)} {
			next := interpreter.clone()
			if next.restrict(guard) {
				next.enter(instruction.Block().Succs[i])
				states = append(states, next)
			}
		}
		return states
	case *ssa.Jump:
		interpreter.enter(instruction.Block().Succs[0])
	case *ssa.Return:
		frame := interpreter.frame()
		for _, value := range instruction.Results {
			frame.ReturnValues = append(frame.ReturnValues, interpreter.resolveExpression(value))
		}
		if len(frame.ReturnValues) > 0 {
			frame.ReturnValue = frame.ReturnValues[0]
		}
		interpreter.Status = Returned
	case *ssa.Panic:
		interpreter.Status = Panicked
		interpreter.Diagnostic = "explicit panic"
		if boxed, ok := instruction.X.(*ssa.MakeInterface); ok {
			if literal, ok := boxed.X.(*ssa.Const); ok {
				interpreter.Diagnostic = literal.Value.ExactString()
			}
		}
	case *ssa.MakeInterface:
		// SSA boxes panic arguments even though general interface operations are excluded.
		for _, referrer := range *instruction.Referrers() {
			if _, ok := referrer.(*ssa.Panic); !ok {
				panic("analysis: interface operations are unsupported")
			}
		}
		interpreter.frame().InstructionIndex++
	case *ssa.DebugRef:
		interpreter.frame().InstructionIndex++
	case *ssa.BinOp:
		if guard, message := interpreter.arithmeticGuard(instruction); guard != nil {
			failed := interpreter.clone()
			states := []Interpreter{}
			if failed.restrict(negate(guard)) {
				failed.Status = Panicked
				failed.Diagnostic = message
				states = append(states, failed)
			}
			if interpreter.restrict(guard) {
				interpreter.assign(instruction)
				states = append(states, *interpreter)
			}
			return states
		}
		interpreter.assign(instruction)
	case *ssa.UnOp:
		interpreter.assign(instruction)
	case *ssa.Convert:
		interpreter.assign(instruction)
	case *ssa.ChangeType:
		interpreter.assign(instruction)
	default:
		panic(fmt.Sprintf("analysis: unsupported instruction %T: %s", element, element))
	}
	return []Interpreter{*interpreter}
}

func (interpreter *Interpreter) arithmeticGuard(instruction *ssa.BinOp) (symbolic.SymbolicExpression, string) {
	right := interpreter.resolveExpression(instruction.Y)
	switch instruction.Op {
	case token.QUO, token.REM:
		return interpreter.fold(symbolic.NewBinaryOperation(right, symbolic.NewTypedIntConstant(0, right.Type()), symbolic.NE)), "integer divide by zero"
	case token.SHL, token.SHR:
		if right.Type().IsSigned() {
			return interpreter.fold(symbolic.NewBinaryOperation(right, symbolic.NewTypedIntConstant(0, right.Type()), symbolic.GE)), "negative shift count"
		}
	}
	return nil, ""
}

func (interpreter *Interpreter) assign(value ssa.Value) {
	expression := interpreter.evaluate(value)
	interpreter.frame().LocalMemory[valueKey(value)] = interpreter.fold(expression)
	interpreter.frame().InstructionIndex++
}

func (interpreter *Interpreter) resolveExpression(value ssa.Value) symbolic.SymbolicExpression {
	if literal, ok := value.(*ssa.Const); ok {
		t := expressionType(literal.Type())
		if t == symbolic.BoolType {
			return symbolic.NewBoolConstant(constant.BoolVal(literal.Value))
		}
		number, ok := new(big.Int).SetString(literal.Value.ExactString(), 10)
		if !ok {
			panic("analysis: invalid integer constant")
		}
		return symbolic.NewBigIntConstant(number, t)
	}
	if expression, ok := interpreter.frame().LocalMemory[valueKey(value)]; ok {
		return expression
	}
	panic(fmt.Sprintf("analysis: unresolved SSA value %s (%T)", value.Name(), value))
}

func (interpreter *Interpreter) evaluate(value ssa.Value) symbolic.SymbolicExpression {
	switch instruction := value.(type) {
	case *ssa.BinOp:
		left, right := interpreter.resolveExpression(instruction.X), interpreter.resolveExpression(instruction.Y)
		operators := map[token.Token]symbolic.BinaryOperator{
			token.ADD: symbolic.ADD, token.SUB: symbolic.SUB, token.MUL: symbolic.MUL, token.QUO: symbolic.DIV, token.REM: symbolic.MOD,
			token.AND: symbolic.BIT_AND, token.OR: symbolic.BIT_OR, token.XOR: symbolic.BIT_XOR, token.SHL: symbolic.SHL, token.SHR: symbolic.SHR,
			token.EQL: symbolic.EQ, token.NEQ: symbolic.NE, token.LSS: symbolic.LT, token.LEQ: symbolic.LE, token.GTR: symbolic.GT, token.GEQ: symbolic.GE,
		}
		if instruction.Op == token.AND_NOT {
			return symbolic.NewBinaryOperation(left, symbolic.NewUnaryOperation(right, symbolic.BIT_NOT), symbolic.BIT_AND)
		}
		if op, ok := operators[instruction.Op]; ok {
			return symbolic.NewBinaryOperation(left, right, op)
		}
	case *ssa.UnOp:
		operand := interpreter.resolveExpression(instruction.X)
		switch instruction.Op {
		case token.SUB:
			return symbolic.NewUnaryOperation(operand, symbolic.NEG)
		case token.XOR:
			return symbolic.NewUnaryOperation(operand, symbolic.BIT_NOT)
		case token.NOT:
			return negate(operand)
		}
	case *ssa.Convert:
		return symbolic.NewConversion(interpreter.resolveExpression(instruction.X), expressionType(instruction.Type()))
	case *ssa.ChangeType:
		operand := interpreter.resolveExpression(instruction.X)
		target := expressionType(instruction.Type())
		if operand.Type() == target {
			return operand
		}
		return symbolic.NewConversion(operand, target)
	}
	panic(fmt.Sprintf("analysis: unsupported expression %T: %s", value, value))
}

func (interpreter *Interpreter) fold(expression symbolic.SymbolicExpression) symbolic.SymbolicExpression {
	value, err := interpreter.Analyser.Z3Translator.TranslateExpression(expression)
	if err != nil {
		panic(fmt.Sprintf("analysis: %v", err))
	}
	ctx := interpreter.Analyser.Z3Translator.GetContext().(*z3.Context)
	simplified := ctx.Simplify(value.(z3.Value), nil)
	if boolean, ok := simplified.(z3.Bool); ok {
		if literal, ok := boolean.AsBool(); ok {
			return symbolic.NewBoolConstant(literal)
		}
	}
	if integer, ok := simplified.(z3.BV); ok {
		number, literal := integer.AsBigUnsigned()
		if expression.Type().IsSigned() {
			number, literal = integer.AsBigSigned()
		}
		if literal {
			return symbolic.NewBigIntConstant(number, expression.Type())
		}
	}
	return expression
}

func expressionType(t types.Type) symbolic.ExpressionType {
	basic, ok := t.Underlying().(*types.Basic)
	if ok {
		mapping := map[types.BasicKind]symbolic.ExpressionType{
			types.Bool: symbolic.BoolType, types.UntypedBool: symbolic.BoolType,
			types.Int: symbolic.IntType, types.UntypedInt: symbolic.IntType, types.UntypedRune: symbolic.Int32Type,
			types.Int8: symbolic.Int8Type, types.Int16: symbolic.Int16Type, types.Int32: symbolic.Int32Type, types.Int64: symbolic.Int64Type,
			types.Uint: symbolic.UintType, types.Uint8: symbolic.Uint8Type, types.Uint16: symbolic.Uint16Type, types.Uint32: symbolic.Uint32Type, types.Uint64: symbolic.Uint64Type, types.Uintptr: symbolic.UintptrType,
		}
		if result, ok := mapping[basic.Kind()]; ok {
			return result
		}
	}
	panic(fmt.Sprintf("analysis: unsupported type %s (only integers and Booleans are supported)", t))
}

func (interpreter Interpreter) String() string {
	parts := []string{fmt.Sprintf("%s: path %s", interpreter.Status, interpreter.PathCondition)}
	if len(interpreter.CallStack) > 0 {
		values := interpreter.frame().ReturnValues
		if len(values) > 0 {
			returns := make([]string, len(values))
			for i, value := range values {
				returns[i] = value.String()
			}
			parts = append(parts, "return "+strings.Join(returns, ", "))
		}
	}
	if interpreter.Diagnostic != "" {
		parts = append(parts, interpreter.Diagnostic)
	}
	return strings.Join(parts, "; ")
}

func valueKey(value ssa.Value) string {
	if _, ok := value.(*ssa.Parameter); ok {
		return value.Name()
	}
	return "%" + value.Name()
}
