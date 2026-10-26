// Package symbolic содержит конкретные реализации символьных выражений
package symbolic

import (
	"fmt"
	"math/big"
	"strings"
)

// SymbolicExpression - базовый интерфейс для всех символьных выражений
type SymbolicExpression interface {
	// Type возвращает тип выражения
	Type() ExpressionType

	// String возвращает строковое представление выражения
	String() string

	// Accept принимает visitor для обхода дерева выражений
	Accept(visitor Visitor) interface{}
}

// SymbolicVariable представляет символьную переменную
type SymbolicVariable struct {
	Name     string
	ExprType ExpressionType
}

// NewSymbolicVariable создаёт новую символьную переменную
func NewSymbolicVariable(name string, exprType ExpressionType) *SymbolicVariable {
	return &SymbolicVariable{
		Name:     name,
		ExprType: exprType,
	}
}

// Type возвращает тип переменной
func (sv *SymbolicVariable) Type() ExpressionType {
	return sv.ExprType
}

// String возвращает строковое представление переменной
func (sv *SymbolicVariable) String() string {
	return sv.Name
}

// Accept реализует Visitor pattern
func (sv *SymbolicVariable) Accept(visitor Visitor) interface{} {
	return visitor.VisitVariable(sv)
}

// IntConstant представляет целочисленную константу
type IntConstant struct {
	Value    int64
	ExprType ExpressionType
	bigValue *big.Int
}

// NewIntConstant создаёт новую целочисленную константу
func NewIntConstant(value int64) *IntConstant {
	return NewTypedIntConstant(value, IntType)
}

func NewTypedIntConstant(value int64, exprType ExpressionType) *IntConstant {
	return NewBigIntConstant(big.NewInt(value), exprType)
}

func NewBigIntConstant(value *big.Int, exprType ExpressionType) *IntConstant {
	if value == nil {
		panic("integer constant value must not be nil")
	}
	if !exprType.IsInteger() {
		panic(fmt.Sprintf("integer constant cannot have type %s", exprType))
	}
	copyValue := new(big.Int).Set(value)
	return &IntConstant{Value: copyValue.Int64(), ExprType: exprType, bigValue: copyValue}
}

// Type возвращает тип константы
func (ic *IntConstant) Type() ExpressionType {
	return ic.ExprType
}

// String возвращает строковое представление константы
func (ic *IntConstant) String() string {
	return ic.BigIntValue().String()
}

func (ic *IntConstant) BigIntValue() *big.Int {
	if ic.bigValue != nil {
		return new(big.Int).Set(ic.bigValue)
	}
	return big.NewInt(ic.Value)
}

// Accept реализует Visitor pattern
func (ic *IntConstant) Accept(visitor Visitor) interface{} {
	return visitor.VisitIntConstant(ic)
}

// BoolConstant представляет булеву константу
type BoolConstant struct {
	Value bool
}

// NewBoolConstant создаёт новую булеву константу
func NewBoolConstant(value bool) *BoolConstant {
	return &BoolConstant{Value: value}
}

// Type возвращает тип константы
func (bc *BoolConstant) Type() ExpressionType {
	return BoolType
}

// String возвращает строковое представление константы
func (bc *BoolConstant) String() string {
	return fmt.Sprintf("%t", bc.Value)
}

// Accept реализует Visitor pattern
func (bc *BoolConstant) Accept(visitor Visitor) interface{} {
	return visitor.VisitBoolConstant(bc)
}

// BinaryOperation представляет бинарную операцию
type BinaryOperation struct {
	Left     SymbolicExpression
	Right    SymbolicExpression
	Operator BinaryOperator
}

// NewBinaryOperation создаёт новую бинарную операцию
func NewBinaryOperation(left, right SymbolicExpression, op BinaryOperator) *BinaryOperation {
	if left == nil || right == nil {
		panic("binary operation operands must not be nil")
	}
	leftType, rightType := left.Type(), right.Type()
	switch op {
	case ADD, SUB, MUL, DIV, MOD, BIT_AND, BIT_OR, BIT_XOR:
		requireSameIntegerTypes(leftType, rightType, op.String())
	case SHL, SHR:
		if !leftType.IsInteger() || !rightType.IsInteger() {
			panic(fmt.Sprintf("operator %s requires integer operands", op))
		}
	case LT, LE, GT, GE:
		requireSameIntegerTypes(leftType, rightType, op.String())
	case EQ, NE:
		if leftType != rightType || (!leftType.IsInteger() && leftType != BoolType) {
			panic(fmt.Sprintf("operator %s requires operands of the same primitive type, got %s and %s", op, leftType, rightType))
		}
	default:
		panic(fmt.Sprintf("unsupported binary operator %d", op))
	}
	return &BinaryOperation{Left: left, Right: right, Operator: op}
}

// Type возвращает результирующий тип операции
func (bo *BinaryOperation) Type() ExpressionType {
	switch bo.Operator {
	case EQ, NE, LT, LE, GT, GE:
		return BoolType
	default:
		return bo.Left.Type()
	}
}

// String возвращает строковое представление операции
func (bo *BinaryOperation) String() string {
	return fmt.Sprintf("(%s %s %s)", bo.Left, bo.Operator, bo.Right)
}

// Accept реализует Visitor pattern
func (bo *BinaryOperation) Accept(visitor Visitor) interface{} {
	return visitor.VisitBinaryOperation(bo)
}

// LogicalOperation представляет логическую операцию
type LogicalOperation struct {
	Operands []SymbolicExpression
	Operator LogicalOperator
}

// NewLogicalOperation создаёт новую логическую операцию
func NewLogicalOperation(operands []SymbolicExpression, op LogicalOperator) *LogicalOperation {
	want := 2
	switch op {
	case AND, OR:
		if len(operands) < want {
			panic(fmt.Sprintf("operator %s requires at least two operands", op))
		}
	case NOT:
		if len(operands) != 1 {
			panic("operator ! requires exactly one operand")
		}
	case IMPLIES:
		if len(operands) != want {
			panic("operator => requires exactly two operands")
		}
	default:
		panic(fmt.Sprintf("unsupported logical operator %d", op))
	}
	for _, operand := range operands {
		if operand == nil || operand.Type() != BoolType {
			panic(fmt.Sprintf("operator %s requires boolean operands", op))
		}
	}
	return &LogicalOperation{Operands: append([]SymbolicExpression(nil), operands...), Operator: op}
}

// Type возвращает тип логической операции (всегда bool)
func (lo *LogicalOperation) Type() ExpressionType {
	return BoolType
}

// String возвращает строковое представление логической операции
func (lo *LogicalOperation) String() string {
	if lo.Operator == NOT {
		return "!" + lo.Operands[0].String()
	}
	parts := make([]string, len(lo.Operands))
	for i, operand := range lo.Operands {
		parts[i] = operand.String()
	}
	return "(" + strings.Join(parts, " "+lo.Operator.String()+" ") + ")"
}

// Accept реализует Visitor pattern
func (lo *LogicalOperation) Accept(visitor Visitor) interface{} {
	return visitor.VisitLogicalOperation(lo)
}

// Операторы для бинарных выражений
type BinaryOperator int

const (
	// Арифметические операторы
	ADD BinaryOperator = iota
	SUB
	MUL
	DIV
	MOD
	BIT_AND
	BIT_OR
	BIT_XOR
	SHL
	SHR

	// Операторы сравнения
	EQ // равно
	NE // не равно
	LT // меньше
	LE // меньше или равно
	GT // больше
	GE // больше или равно
)

// String возвращает строковое представление оператора
func (op BinaryOperator) String() string {
	switch op {
	case ADD:
		return "+"
	case SUB:
		return "-"
	case MUL:
		return "*"
	case DIV:
		return "/"
	case MOD:
		return "%"
	case BIT_AND:
		return "&"
	case BIT_OR:
		return "|"
	case BIT_XOR:
		return "^"
	case SHL:
		return "<<"
	case SHR:
		return ">>"
	case EQ:
		return "=="
	case NE:
		return "!="
	case LT:
		return "<"
	case LE:
		return "<="
	case GT:
		return ">"
	case GE:
		return ">="
	default:
		return "unknown"
	}
}

type UnaryOperation struct {
	Operand  SymbolicExpression
	Operator UnaryOperator
}

type UnaryOperator int

const (
	NEG UnaryOperator = iota
	BIT_NOT
)

func (op UnaryOperator) String() string {
	switch op {
	case NEG:
		return "-"
	case BIT_NOT:
		return "^"
	default:
		return "unknown"
	}
}

func NewUnaryOperation(operand SymbolicExpression, op UnaryOperator) *UnaryOperation {
	if operand == nil || !operand.Type().IsInteger() {
		panic(fmt.Sprintf("unary operator %s requires an integer operand", op))
	}
	if op != NEG && op != BIT_NOT {
		panic(fmt.Sprintf("unsupported unary operator %d", op))
	}
	return &UnaryOperation{Operand: operand, Operator: op}
}

func (uo *UnaryOperation) Type() ExpressionType { return uo.Operand.Type() }
func (uo *UnaryOperation) String() string {
	return fmt.Sprintf("(%s%s)", uo.Operator, uo.Operand)
}
func (uo *UnaryOperation) Accept(visitor Visitor) interface{} {
	return visitor.VisitUnaryOperation(uo)
}

type Conversion struct {
	Operand    SymbolicExpression
	TargetType ExpressionType
}

func NewConversion(operand SymbolicExpression, targetType ExpressionType) *Conversion {
	if operand == nil || !operand.Type().IsInteger() || !targetType.IsInteger() {
		panic("integer conversion requires integer source and target types")
	}
	return &Conversion{Operand: operand, TargetType: targetType}
}

func (c *Conversion) Type() ExpressionType { return c.TargetType }
func (c *Conversion) String() string {
	return fmt.Sprintf("%s(%s)", c.TargetType, c.Operand)
}
func (c *Conversion) Accept(visitor Visitor) interface{} { return visitor.VisitConversion(c) }

func requireSameIntegerTypes(left, right ExpressionType, operator string) {
	if !left.IsInteger() || left != right {
		panic(fmt.Sprintf("operator %s requires matching integer operands, got %s and %s", operator, left, right))
	}
}

// Логические операторы
type LogicalOperator int

const (
	AND LogicalOperator = iota
	OR
	NOT
	IMPLIES
)

// String возвращает строковое представление логического оператора
func (op LogicalOperator) String() string {
	switch op {
	case AND:
		return "&&"
	case OR:
		return "||"
	case NOT:
		return "!"
	case IMPLIES:
		return "=>"
	default:
		return "unknown"
	}
}

type Ref struct {
	// TODO: Выбрать и написать внутреннее представление символьной ссылки
}

func (ref *Ref) Type() ExpressionType {
	panic("не реализовано")
}

func (ref *Ref) String() string {
	panic("не реализовано")
}

func (ref *Ref) Accept(visitor Visitor) interface{} {
	panic("не реализовано")
}

// TODO: Добавьте дополнительные типы выражений по необходимости:
// - UnaryOperation (унарные операции: -x, !x)
// - ArrayAccess (доступ к элементам массива: arr[index])
// - FunctionCall (вызовы функций: f(x, y))
// - ConditionalExpression (тернарный оператор: condition ? true_expr : false_expr)
