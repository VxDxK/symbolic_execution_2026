package symbolic_test

import (
	"math/big"
	"strconv"
	"testing"

	s "symbolic-execution-course/internal/symbolic"
)

func TestIntegerTypes(t *testing.T) {
	for _, tt := range []struct {
		typeOf s.ExpressionType
		name   string
		width  int
		signed bool
	}{
		{s.IntType, "int", strconv.IntSize, true}, {s.UintType, "uint", strconv.IntSize, false},
		{s.UintptrType, "uintptr", strconv.IntSize, false},
		{s.Int8Type, "int8", 8, true}, {s.Uint8Type, "uint8", 8, false},
		{s.Int16Type, "int16", 16, true}, {s.Uint16Type, "uint16", 16, false},
		{s.Int32Type, "int32", 32, true}, {s.Uint32Type, "uint32", 32, false},
		{s.Int64Type, "int64", 64, true}, {s.Uint64Type, "uint64", 64, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.typeOf.String() != tt.name || tt.typeOf.BitWidth() != tt.width || tt.typeOf.IsSigned() != tt.signed || !tt.typeOf.IsInteger() {
				t.Fatalf("incorrect metadata for %s", tt.name)
			}
		})
	}
	if s.ByteType != s.Uint8Type || s.RuneType != s.Int32Type {
		t.Fatal("incorrect Go aliases")
	}
	for _, typ := range []s.ExpressionType{s.BoolType, s.ArrayType, s.ExpressionType(-1)} {
		if typ.IsInteger() || typ.BitWidth() != 0 || typ.IsSigned() {
			t.Fatalf("non-integer metadata for %s", typ)
		}
	}
}

func TestExpressionTypesAndStrings(t *testing.T) {
	x, y := s.NewSymbolicVariable("x", s.IntType), s.NewSymbolicVariable("y", s.IntType)
	for _, tt := range []struct {
		op     s.BinaryOperator
		text   string
		result s.ExpressionType
	}{
		{s.ADD, "+", s.IntType}, {s.SUB, "-", s.IntType}, {s.MUL, "*", s.IntType}, {s.DIV, "/", s.IntType}, {s.MOD, "%", s.IntType},
		{s.BIT_AND, "&", s.IntType}, {s.BIT_OR, "|", s.IntType}, {s.BIT_XOR, "^", s.IntType}, {s.SHL, "<<", s.IntType}, {s.SHR, ">>", s.IntType},
		{s.EQ, "==", s.BoolType}, {s.NE, "!=", s.BoolType}, {s.LT, "<", s.BoolType}, {s.LE, "<=", s.BoolType}, {s.GT, ">", s.BoolType}, {s.GE, ">=", s.BoolType},
	} {
		t.Run(tt.text, func(t *testing.T) {
			expr := s.NewBinaryOperation(x, y, tt.op)
			if expr.Type() != tt.result || expr.String() != "(x "+tt.text+" y)" {
				t.Fatalf("unexpected expression: %s (%s)", expr, expr.Type())
			}
		})
	}
	a, b := s.NewSymbolicVariable("a", s.BoolType), s.NewSymbolicVariable("b", s.BoolType)
	for _, tt := range []struct {
		expr s.SymbolicExpression
		text string
		typ  s.ExpressionType
	}{
		{x, "x", s.IntType}, {s.NewIntConstant(-42), "-42", s.IntType}, {s.NewBoolConstant(true), "true", s.BoolType}, {s.NewBoolConstant(false), "false", s.BoolType},
		{s.NewLogicalOperation([]s.SymbolicExpression{a, b}, s.AND), "(a && b)", s.BoolType},
		{s.NewLogicalOperation([]s.SymbolicExpression{a, b, a}, s.OR), "(a || b || a)", s.BoolType},
		{s.NewLogicalOperation([]s.SymbolicExpression{a}, s.NOT), "!a", s.BoolType},
		{s.NewLogicalOperation([]s.SymbolicExpression{a, b}, s.IMPLIES), "(a => b)", s.BoolType},
		{s.NewUnaryOperation(x, s.NEG), "(-x)", s.IntType}, {s.NewUnaryOperation(x, s.BIT_NOT), "(^x)", s.IntType},
		{s.NewConversion(x, s.Uint8Type), "uint8(x)", s.Uint8Type},
		{s.NewBinaryOperation(s.NewBinaryOperation(x, y, s.ADD), s.NewIntConstant(5), s.GT), "((x + y) > 5)", s.BoolType},
	} {
		t.Run(tt.text, func(t *testing.T) {
			if tt.expr.String() != tt.text || tt.expr.Type() != tt.typ {
				t.Fatalf("got %s (%s)", tt.expr, tt.expr.Type())
			}
		})
	}
}

func TestConstantAndOperandCopies(t *testing.T) {
	value, _ := new(big.Int).SetString("18446744073709551615", 10)
	constant := s.NewBigIntConstant(value, s.Uint64Type)
	value.SetInt64(0)
	constant.BigIntValue().SetInt64(1)
	if constant.String() != "18446744073709551615" || constant.Type() != s.Uint64Type {
		t.Fatal("big integer value was not preserved")
	}
	operands := []s.SymbolicExpression{s.NewBoolConstant(true), s.NewBoolConstant(false)}
	expr := s.NewLogicalOperation(operands, s.AND)
	operands[0] = s.NewBoolConstant(false)
	if expr.String() != "(true && false)" {
		t.Fatal("logical expression retained caller's operand slice")
	}
}

func TestConstructorValidation(t *testing.T) {
	i, b := s.NewIntConstant(1), s.NewBoolConstant(true)
	for name, build := range map[string]func(){
		"nil left": func() { s.NewBinaryOperation(nil, i, s.ADD) }, "nil right": func() { s.NewBinaryOperation(i, nil, s.ADD) },
		"mixed widths":       func() { s.NewBinaryOperation(i, s.NewTypedIntConstant(1, s.Int8Type), s.ADD) },
		"mixed signs":        func() { s.NewBinaryOperation(i, s.NewTypedIntConstant(1, s.UintType), s.EQ) },
		"boolean arithmetic": func() { s.NewBinaryOperation(b, b, s.ADD) }, "boolean ordering": func() { s.NewBinaryOperation(b, b, s.LT) },
		"boolean shift": func() { s.NewBinaryOperation(i, b, s.SHL) }, "unknown binary": func() { s.NewBinaryOperation(i, i, s.BinaryOperator(-1)) },
		"empty and": func() { s.NewLogicalOperation(nil, s.AND) }, "singleton or": func() { s.NewLogicalOperation([]s.SymbolicExpression{b}, s.OR) },
		"empty not": func() { s.NewLogicalOperation(nil, s.NOT) }, "binary not": func() { s.NewLogicalOperation([]s.SymbolicExpression{b, b}, s.NOT) },
		"singleton implies": func() { s.NewLogicalOperation([]s.SymbolicExpression{b}, s.IMPLIES) },
		"integer logic":     func() { s.NewLogicalOperation([]s.SymbolicExpression{b, i}, s.AND) }, "nil logic": func() { s.NewLogicalOperation([]s.SymbolicExpression{b, nil}, s.AND) },
		"unknown logic": func() { s.NewLogicalOperation([]s.SymbolicExpression{b, b}, s.LogicalOperator(-1)) },
		"nil unary":     func() { s.NewUnaryOperation(nil, s.NEG) }, "boolean unary": func() { s.NewUnaryOperation(b, s.NEG) }, "unknown unary": func() { s.NewUnaryOperation(i, s.UnaryOperator(-1)) },
		"nil conversion": func() { s.NewConversion(nil, s.IntType) }, "boolean source": func() { s.NewConversion(b, s.IntType) }, "boolean target": func() { s.NewConversion(i, s.BoolType) },
		"nil constant": func() { s.NewBigIntConstant(nil, s.IntType) }, "boolean constant type": func() { s.NewTypedIntConstant(1, s.BoolType) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected constructor to panic")
				}
			}()
			build()
		})
	}
}

type dispatchVisitor struct {
	kind string
	expr s.SymbolicExpression
}

func (v *dispatchVisitor) record(kind string, expr s.SymbolicExpression) interface{} {
	v.kind, v.expr = kind, expr
	return "visited"
}
func (v *dispatchVisitor) VisitVariable(e *s.SymbolicVariable) interface{} {
	return v.record("variable", e)
}
func (v *dispatchVisitor) VisitIntConstant(e *s.IntConstant) interface{} {
	return v.record("integer", e)
}
func (v *dispatchVisitor) VisitBoolConstant(e *s.BoolConstant) interface{} {
	return v.record("boolean", e)
}
func (v *dispatchVisitor) VisitBinaryOperation(e *s.BinaryOperation) interface{} {
	return v.record("binary", e)
}
func (v *dispatchVisitor) VisitLogicalOperation(e *s.LogicalOperation) interface{} {
	return v.record("logical", e)
}
func (v *dispatchVisitor) VisitUnaryOperation(e *s.UnaryOperation) interface{} {
	return v.record("unary", e)
}
func (v *dispatchVisitor) VisitConversion(e *s.Conversion) interface{} {
	return v.record("conversion", e)
}

func (v *dispatchVisitor) VisitRef(e *s.Ref) interface{} { return v.record("Ref", e) }
func (v *dispatchVisitor) VisitArrayVariable(e *s.ArrayVariable) interface{} {
	return v.record("ArrayVariable", e)
}
func (v *dispatchVisitor) VisitConstantArray(e *s.ConstantArray) interface{} {
	return v.record("ConstantArray", e)
}
func (v *dispatchVisitor) VisitArraySelect(e *s.ArraySelect) interface{} {
	return v.record("ArraySelect", e)
}
func (v *dispatchVisitor) VisitArrayStore(e *s.ArrayStore) interface{} {
	return v.record("ArrayStore", e)
}

func TestVisitorDispatch(t *testing.T) {
	i, b := s.NewIntConstant(1), s.NewBoolConstant(true)
	a := s.NewArrayVariable("a", s.ArrayLayout(s.ScalarType(s.IntType)))
	for kind, expr := range map[string]s.SymbolicExpression{
		"variable": s.NewSymbolicVariable("x", s.IntType), "integer": i, "boolean": b,
		"Ref": s.NewNilRef(s.ScalarType(s.IntType)), "ArrayVariable": a,
		"ConstantArray": s.NewConstantArray(s.ScalarType(s.IntType), i),
		"ArraySelect":   s.NewArraySelect(a, i), "ArrayStore": s.NewArrayStore(a, i, i),
		"binary": s.NewBinaryOperation(i, i, s.ADD), "logical": s.NewLogicalOperation([]s.SymbolicExpression{b}, s.NOT),
		"unary": s.NewUnaryOperation(i, s.NEG), "conversion": s.NewConversion(i, s.Int8Type),
	} {
		t.Run(kind, func(t *testing.T) {
			v := &dispatchVisitor{}
			result := expr.Accept(v)
			if result != "visited" || v.kind != kind || v.expr != expr {
				t.Fatalf("incorrect visitor dispatch: %s", v.kind)
			}
		})
	}
}

func TestMemoryExpressionTypesAndValidation(t *testing.T) {
	integer := s.ScalarType(s.IntType)
	fields := []*s.TypeInfo{integer}
	layout := s.StructLayout("Immutable", fields...)
	fields[0] = s.ScalarType(s.BoolType)
	exposed := layout.Fields()
	exposed[0] = s.ScalarType(s.UintType)
	if !layout.Fields()[0].Equal(integer) {
		t.Fatal("layout retained mutable field slice")
	}
	ref := s.NewSymbolicRef("p", layout)
	if ref.Type() != s.RefType || ref.String() != "ref(p)" || !s.TypeOf(ref).Equal(s.ReferenceType(layout)) {
		t.Fatal("incorrect reference type")
	}
	array := s.NewArrayVariable("refs", s.ArrayLayout(s.ReferenceType(layout)))
	selected := s.NewArraySelect(array, s.NewIntConstant(0))
	if selected.Type() != s.RefType || !s.TypeOf(selected).Equal(s.ReferenceType(layout)) {
		t.Fatal("incorrect reference select type")
	}
	scalarArray := s.NewArrayVariable("ints", s.ArrayLayout(integer))
	for name, build := range map[string]func(){
		"reference arithmetic":        func() { s.NewBinaryOperation(ref, ref, s.ADD) },
		"reference ordering":          func() { s.NewBinaryOperation(ref, ref, s.LT) },
		"different reference targets": func() { s.NewBinaryOperation(ref, s.NewNilRef(integer), s.EQ) },
		"signed address":              func() { s.NewRef(s.NewIntConstant(1), layout) },
		"missing target":              func() { s.NewNilRef(nil) },
		"missing domain":              func() { s.ArraySort(nil, integer) },
		"aggregate sort":              func() { s.ArraySort(integer, layout) },
		"anonymous layout":            func() { s.StructLayout("", integer) },
		"aggregate field":             func() { s.StructLayout("Nested", layout) },
		"non-array variable":          func() { s.NewArrayVariable("x", integer) },
		"untyped select":              func() { s.NewArraySelect(s.NewSymbolicVariable("x", s.ArrayType), s.NewIntConstant(0)) },
		"wrong index sort":            func() { s.NewArraySelect(scalarArray, s.NewTypedIntConstant(0, s.UintType)) },
		"wrong element sort":          func() { s.NewArrayStore(scalarArray, s.NewIntConstant(0), s.NewBoolConstant(true)) },
		"wrong reference element":     func() { s.NewArrayStore(array, s.NewIntConstant(0), s.NewNilRef(integer)) },
		"nil array":                   func() { s.NewArraySelect(nil, s.NewIntConstant(0)) },
		"nil index":                   func() { s.NewArraySelect(scalarArray, nil) },
		"nil value":                   func() { s.NewArrayStore(scalarArray, s.NewIntConstant(0), nil) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected constructor panic")
				}
			}()
			build()
		})
	}
}
