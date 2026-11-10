package translator_test

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ebukreev/go-z3/z3"
	s "symbolic-execution-course/internal/symbolic"
	tr "symbolic-execution-course/internal/translator"
)

func translate(t *testing.T, translator *tr.Z3Translator, expr s.SymbolicExpression) z3.Value {
	t.Helper()
	value, err := translator.TranslateExpression(expr)
	if err != nil {
		t.Fatalf("translate %s: %v", expr, err)
	}
	result, ok := value.(z3.Value)
	if !ok {
		t.Fatalf("translation returned %T", value)
	}
	return result
}

func checkSAT(t *testing.T, solver *z3.Solver, want bool) {
	t.Helper()
	got, err := solver.Check()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("satisfiable = %t, want %t", got, want)
	}
}

func evaluate(t *testing.T, expr s.SymbolicExpression) z3.Value {
	t.Helper()
	translator := tr.NewZ3Translator()
	t.Cleanup(translator.Close)
	value := translate(t, translator, expr)
	solver := z3.NewSolver(translator.GetContext().(*z3.Context))
	checkSAT(t, solver, true)
	return solver.Model().Eval(value, true)
}

func integer(value int64, typ s.ExpressionType) *s.IntConstant {
	return s.NewTypedIntConstant(value, typ)
}

func assertInteger(t *testing.T, expr s.SymbolicExpression, want int64) {
	t.Helper()
	value, ok := evaluate(t, expr).(z3.BV)
	if !ok {
		t.Fatal("expected bit-vector result")
	}
	if value.Sort().BVSize() != expr.Type().BitWidth() {
		t.Fatal("incorrect result width")
	}
	if expr.Type().IsSigned() {
		got, literal, valid := value.AsInt64()
		if !literal || !valid || got != want {
			t.Fatalf("%s = %d, want %d", expr, got, want)
		}
	} else {
		got, literal, valid := value.AsUint64()
		if !literal || !valid || got != uint64(want) {
			t.Fatalf("%s = %d, want %d", expr, got, want)
		}
	}
}

func assertBoolean(t *testing.T, expr s.SymbolicExpression, want bool) {
	t.Helper()
	value, ok := evaluate(t, expr).(z3.Bool)
	if !ok {
		t.Fatal("expected boolean result")
	}
	got, literal := value.AsBool()
	if !literal || got != want {
		t.Fatalf("%s = %t, want %t", expr, got, want)
	}
}

func TestTranslationSortsAndLiterals(t *testing.T) {
	translator := tr.NewZ3Translator()
	defer translator.Close()
	for _, typ := range []s.ExpressionType{s.IntType, s.UintType, s.UintptrType, s.Int8Type, s.Uint8Type, s.Int16Type, s.Uint16Type, s.Int32Type, s.Uint32Type, s.Int64Type, s.Uint64Type} {
		t.Run(typ.String(), func(t *testing.T) {
			for _, expr := range []s.SymbolicExpression{s.NewSymbolicVariable(typ.String(), typ), integer(42, typ)} {
				value, ok := translate(t, translator, expr).(z3.BV)
				if !ok || value.Sort().BVSize() != typ.BitWidth() {
					t.Fatalf("wrong Z3 sort for %s", typ)
				}
			}
			assertInteger(t, integer(42, typ), 42)
		})
	}
	if _, ok := translate(t, translator, s.NewSymbolicVariable("flag", s.BoolType)).(z3.Bool); !ok {
		t.Fatal("boolean variable has wrong sort")
	}
	for _, value := range []bool{false, true} {
		assertBoolean(t, s.NewBoolConstant(value), value)
	}
	maximum, _ := new(big.Int).SetString("18446744073709551615", 10)
	value := evaluate(t, s.NewBigIntConstant(maximum, s.Uint64Type)).(z3.BV)
	got, literal, valid := value.AsUint64()
	if !literal || !valid || got != ^uint64(0) {
		t.Fatalf("uint64 maximum translated as %d", got)
	}
}

func TestIntegerOperations(t *testing.T) {
	for _, tt := range []struct {
		name              string
		typ               s.ExpressionType
		op                s.BinaryOperator
		left, right, want int64
	}{
		{"add", s.Int8Type, s.ADD, 7, 3, 10}, {"subtract", s.Int8Type, s.SUB, 7, 3, 4}, {"multiply", s.Int8Type, s.MUL, 7, 3, 21},
		{"signed overflow", s.Int8Type, s.ADD, 127, 1, -128}, {"signed underflow", s.Int8Type, s.SUB, -128, 1, 127},
		{"unsigned overflow", s.Uint8Type, s.ADD, 255, 1, 0}, {"unsigned underflow", s.Uint8Type, s.SUB, 0, 1, 255},
		{"multiply overflow", s.Int8Type, s.MUL, 64, 4, 0},
		{"positive division", s.Int8Type, s.DIV, 5, 2, 2}, {"negative dividend", s.Int8Type, s.DIV, -5, 2, -2},
		{"negative divisor", s.Int8Type, s.DIV, 5, -2, -2}, {"both negative", s.Int8Type, s.DIV, -5, -2, 2},
		{"division overflow", s.Int8Type, s.DIV, -128, -1, -128},
		{"positive remainder", s.Int8Type, s.MOD, 5, 2, 1}, {"negative remainder", s.Int8Type, s.MOD, -5, 2, -1},
		{"negative divisor remainder", s.Int8Type, s.MOD, 5, -2, 1}, {"both negative remainder", s.Int8Type, s.MOD, -5, -2, -1},
		{"unsigned division", s.Uint8Type, s.DIV, 255, 2, 127}, {"unsigned remainder", s.Uint8Type, s.MOD, 255, 2, 1},
		{"and", s.Uint8Type, s.BIT_AND, 12, 10, 8}, {"or", s.Uint8Type, s.BIT_OR, 12, 10, 14}, {"xor", s.Uint8Type, s.BIT_XOR, 12, 10, 6},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertInteger(t, s.NewBinaryOperation(integer(tt.left, tt.typ), integer(tt.right, tt.typ), tt.op), tt.want)
		})
	}
	for _, tt := range []struct {
		name        string
		typ         s.ExpressionType
		op          s.UnaryOperator
		input, want int64
	}{
		{"negate", s.Int8Type, s.NEG, 5, -5}, {"minimum negation", s.Int8Type, s.NEG, -128, -128},
		{"unsigned negation", s.Uint8Type, s.NEG, 1, 255}, {"signed complement", s.Int8Type, s.BIT_NOT, 0, -1},
		{"unsigned complement", s.Uint8Type, s.BIT_NOT, 15, 240},
	} {
		t.Run(tt.name, func(t *testing.T) { assertInteger(t, s.NewUnaryOperation(integer(tt.input, tt.typ), tt.op), tt.want) })
	}
}

func TestIntegerComparisons(t *testing.T) {
	for _, typ := range []s.ExpressionType{s.Int8Type, s.Uint8Type} {
		for _, pair := range [][2]int64{{0, 0}, {1, 2}, {2, 1}, {127, 128}, {255, 0}} {
			left, right := pair[0], pair[1]
			if typ.IsSigned() {
				left, right = int64(int8(left)), int64(int8(right))
			}
			for _, tt := range []struct {
				op   s.BinaryOperator
				want bool
			}{
				{s.EQ, left == right}, {s.NE, left != right}, {s.LT, left < right}, {s.LE, left <= right}, {s.GT, left > right}, {s.GE, left >= right},
			} {
				t.Run(fmt.Sprintf("%s/%d%s%d", typ, left, tt.op, right), func(t *testing.T) {
					assertBoolean(t, s.NewBinaryOperation(integer(left, typ), integer(right, typ), tt.op), tt.want)
				})
			}
		}
	}
}

func TestBooleanTruthTables(t *testing.T) {
	for _, a := range []bool{false, true} {
		assertBoolean(t, s.NewLogicalOperation([]s.SymbolicExpression{s.NewBoolConstant(a)}, s.NOT), !a)
		for _, b := range []bool{false, true} {
			left, right := s.NewBoolConstant(a), s.NewBoolConstant(b)
			for _, tt := range []struct {
				op   s.LogicalOperator
				want bool
			}{{s.AND, a && b}, {s.OR, a || b}, {s.IMPLIES, !a || b}} {
				t.Run(fmt.Sprintf("%t%s%t", a, tt.op, b), func(t *testing.T) {
					assertBoolean(t, s.NewLogicalOperation([]s.SymbolicExpression{left, right}, tt.op), tt.want)
				})
			}
			assertBoolean(t, s.NewBinaryOperation(left, right, s.EQ), a == b)
			assertBoolean(t, s.NewBinaryOperation(left, right, s.NE), a != b)
		}
	}
	for _, op := range []s.LogicalOperator{s.AND, s.OR} {
		assertBoolean(t, s.NewLogicalOperation([]s.SymbolicExpression{s.NewBoolConstant(true), s.NewBoolConstant(false), s.NewBoolConstant(true)}, op), op == s.OR)
	}
}

func TestShifts(t *testing.T) {
	for _, typ := range []s.ExpressionType{s.Int8Type, s.Uint8Type} {
		for _, countType := range []s.ExpressionType{s.Uint8Type, s.Int64Type} {
			for _, count := range []int64{0, 7, 8, 9, 256} {
				if countType == s.Uint8Type && count == 256 {
					continue
				}
				for _, op := range []s.BinaryOperator{s.SHL, s.SHR} {
					t.Run(fmt.Sprintf("%s/%s/%s/%d", typ, countType, op, count), func(t *testing.T) {
						var want int64
						if typ.IsSigned() {
							v := int8(-128)
							if op == s.SHL {
								want = int64(v << uint(count))
							} else {
								want = int64(v >> uint(count))
							}
						} else {
							v := uint8(128)
							if op == s.SHL {
								want = int64(v << uint(count))
							} else {
								want = int64(v >> uint(count))
							}
						}
						assertInteger(t, s.NewBinaryOperation(integer(128, typ), integer(count, countType), op), want)
					})
				}
			}
		}
	}
	assertInteger(t, s.NewBinaryOperation(integer(1, s.Int64Type), integer(40, s.Uint8Type), s.SHL), 1<<40)
}

func TestConversions(t *testing.T) {
	for _, tt := range []struct {
		name           string
		source, target s.ExpressionType
		input, want    int64
	}{
		{"sign extension", s.Int8Type, s.Int16Type, -1, -1}, {"zero extension", s.Uint8Type, s.Int16Type, 255, 255},
		{"narrowing", s.Int16Type, s.Uint8Type, 511, 255}, {"signed narrowing", s.Int16Type, s.Int8Type, 128, -128},
		{"signed to unsigned", s.Int8Type, s.Uint8Type, -1, 255}, {"unsigned to signed", s.Uint8Type, s.Int8Type, 255, -1},
		{"signed widening to unsigned", s.Int8Type, s.Uint16Type, -1, 65535}, {"same type", s.Int16Type, s.Int16Type, -42, -42},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertInteger(t, s.NewConversion(integer(tt.input, tt.source), tt.target), tt.want)
		})
	}
}

func TestVariableCacheResetAndErrors(t *testing.T) {
	translator := tr.NewZ3Translator()
	defer translator.Close()
	x := s.NewSymbolicVariable("x", s.Int8Type)
	first := translate(t, translator, x)
	second := translate(t, translator, s.NewSymbolicVariable("x", s.Int8Type))
	if !first.AsAST().Equal(second.AsAST()) {
		t.Fatal("same variable produced different ASTs")
	}
	for name, expr := range map[string]s.SymbolicExpression{
		"same width conflicting sign": s.NewSymbolicVariable("x", s.Uint8Type),
		"conflicting width":           s.NewSymbolicVariable("x", s.Int16Type),
		"conflicting kind":            s.NewSymbolicVariable("x", s.BoolType),
		"unsupported array":           s.NewSymbolicVariable("array", s.ArrayType), "nil expression": nil,
	} {
		t.Run(name, func(t *testing.T) {
			value, err := translator.TranslateExpression(expr)
			var translationError *tr.TranslationError
			if value != nil || !errors.As(err, &translationError) {
				t.Fatalf("got (%v, %v), want TranslationError", value, err)
			}
			translate(t, translator, x)
		})
	}
	nested := s.NewBinaryOperation(x, s.NewSymbolicVariable("x", s.Int16Type), s.SHL)
	if value, err := translator.TranslateExpression(nested); value != nil || err == nil {
		t.Fatal("nested variable conflict was not propagated")
	}
	translator.Reset()
	translate(t, translator, s.NewSymbolicVariable("x", s.Uint8Type))
}

func TestArithmeticConstraintModel(t *testing.T) {
	translator := tr.NewZ3Translator()
	defer translator.Close()
	x, y := s.NewSymbolicVariable("x", s.Int16Type), s.NewSymbolicVariable("y", s.Int16Type)
	constraint := s.NewLogicalOperation([]s.SymbolicExpression{
		s.NewBinaryOperation(s.NewBinaryOperation(x, y, s.ADD), integer(10, s.Int16Type), s.EQ),
		s.NewBinaryOperation(x, integer(4, s.Int16Type), s.EQ),
	}, s.AND)
	solver := z3.NewSolver(translator.GetContext().(*z3.Context))
	solver.Assert(translate(t, translator, constraint).(z3.Bool))
	checkSAT(t, solver, true)
	model := solver.Model()
	for _, tt := range []struct {
		expr s.SymbolicExpression
		want int64
	}{{x, 4}, {y, 6}} {
		value := model.Eval(translate(t, translator, tt.expr), true).(z3.BV)
		got, literal, valid := value.AsInt64()
		if !literal || !valid || got != tt.want {
			t.Fatalf("model %s = %d, want %d", tt.expr, got, tt.want)
		}
	}
	solver.Assert(translate(t, translator, s.NewBinaryOperation(y, integer(7, s.Int16Type), s.EQ)).(z3.Bool))
	checkSAT(t, solver, false)
}

func TestSymbolicIdentities(t *testing.T) {
	for name, build := range map[string]func(s.SymbolicExpression) s.SymbolicExpression{
		"add zero": func(x s.SymbolicExpression) s.SymbolicExpression {
			return s.NewBinaryOperation(s.NewBinaryOperation(x, integer(0, s.Int8Type), s.ADD), x, s.EQ)
		},
		"xor self": func(x s.SymbolicExpression) s.SymbolicExpression {
			return s.NewBinaryOperation(s.NewBinaryOperation(x, x, s.BIT_XOR), integer(0, s.Int8Type), s.EQ)
		},
	} {
		t.Run(name, func(t *testing.T) {
			translator := tr.NewZ3Translator()
			defer translator.Close()
			x := s.NewSymbolicVariable("x", s.Int8Type)
			formula := translate(t, translator, build(x)).(z3.Bool)
			solver := z3.NewSolver(translator.GetContext().(*z3.Context))
			solver.Assert(formula.Not())
			checkSAT(t, solver, false)
		})
	}
}

func TestSolverDiscoversOverflow(t *testing.T) {
	translator := tr.NewZ3Translator()
	defer translator.Close()
	x := s.NewSymbolicVariable("x", s.Int8Type)
	sum := s.NewBinaryOperation(x, integer(1, s.Int8Type), s.ADD)
	formula := s.NewBinaryOperation(sum, x, s.LT)
	solver := z3.NewSolver(translator.GetContext().(*z3.Context))
	solver.Assert(translate(t, translator, formula).(z3.Bool))
	checkSAT(t, solver, true)
	model := solver.Model()
	input, literal, valid := model.Eval(translate(t, translator, x), true).(z3.BV).AsInt64()
	if !literal || !valid || input != 127 {
		t.Fatalf("overflow input = %d, want 127", input)
	}
	bits, literal, valid := model.Eval(translate(t, translator, sum), true).(z3.BV).AsUint64()
	if !literal || !valid || bits != 128 {
		t.Fatalf("overflow result bits = %d, want 128", bits)
	}
}

func TestTypedSMTArrays(t *testing.T) {
	translator := tr.NewZ3Translator()
	defer translator.Close()
	intType := s.ScalarType(s.Int16Type)
	indexType := s.ScalarType(s.Uint32Type)
	index := integer(7, s.Uint32Type)
	value := integer(-123, s.Int16Type)
	base := s.NewConstantArray(indexType, integer(0, s.Int16Type))
	updated := s.NewArrayStore(base, index, value)
	assertInteger(t, s.NewArraySelect(updated, index), -123)
	assertInteger(t, s.NewArraySelect(updated, integer(8, s.Uint32Type)), 0)
	nested := s.NewArrayStore(s.NewConstantArray(s.ScalarType(s.IntType), base), s.NewIntConstant(3), updated)
	inner := s.NewArraySelect(nested, s.NewIntConstant(3))
	assertInteger(t, s.NewArraySelect(inner, index), -123)
	booleanArray := s.NewConstantArray(indexType, s.NewBoolConstant(false))
	boolean := s.NewArraySelect(s.NewArrayStore(booleanArray, index, s.NewBoolConstant(true)), index)
	if literal, ok := evaluate(t, boolean).(z3.Bool).AsBool(); !ok || !literal {
		t.Fatal("incorrect boolean array element")
	}

	array := s.NewArrayVariable("typed", s.ArraySort(indexType, intType))
	translated := translate(t, translator, array).(z3.Array)
	if translated.Sort().String() != "(Array (_ BitVec 32) (_ BitVec 16))" {
		t.Fatalf("wrong array sort: %s", translated.Sort())
	}
	layout := s.StructLayout("Target", intType)
	reference := s.NewSymbolicRef("pointer", layout)
	address := translate(t, translator, reference).(z3.BV)
	if address.Sort().BVSize() != s.UintptrType.BitWidth() {
		t.Fatal("reference address has wrong width")
	}
	if bits, _, _ := evaluate(t, s.NewNilRef(layout)).(z3.BV).AsUint64(); bits != 0 {
		t.Fatal("nil reference is not zero")
	}
	referenceArray := s.NewConstantArray(indexType, s.NewNilRef(layout))
	read := s.NewArraySelect(s.NewArrayStore(referenceArray, index, reference), index)
	equality := s.NewBinaryOperation(read, reference, s.EQ)
	solver := z3.NewSolver(translator.GetContext().(*z3.Context))
	solver.Assert(translate(t, translator, equality).(z3.Bool).Not())
	checkSAT(t, solver, false)
}

func TestArrayVariableCacheSignatures(t *testing.T) {
	translator := tr.NewZ3Translator()
	defer translator.Close()
	intType := s.ScalarType(s.IntType)
	original := s.NewArrayVariable("same", s.ArrayLayout(intType))
	first := translate(t, translator, original)
	if again := translate(t, translator, s.NewArrayVariable("same", s.ArrayLayout(intType))); first.String() != again.String() {
		t.Fatal("array cache is unstable")
	}
	for _, conflict := range []s.SymbolicExpression{
		s.NewArrayVariable("same", s.ArrayLayout(s.ScalarType(s.BoolType))),
		s.NewArrayVariable("same", s.ArraySort(s.ScalarType(s.UintType), intType)),
		s.NewArrayVariable("same", s.ArrayLayout(s.ArrayLayout(intType))),
		s.NewSymbolicVariable("same", s.IntType),
	} {
		if _, err := translator.TranslateExpression(conflict); err == nil {
			t.Fatal("expected conflicting variable signature error")
		}
	}
	nestedConflict := s.NewArraySelect(s.NewArrayStore(s.NewArrayVariable("same", s.ArrayLayout(s.ScalarType(s.BoolType))), s.NewIntConstant(0), s.NewBoolConstant(true)), s.NewIntConstant(0))
	if _, err := translator.TranslateExpression(nestedConflict); err == nil {
		t.Fatal("nested translation lost error")
	}
	// An error must not poison the next translation.
	translate(t, translator, original)
	translator.Reset()
	translate(t, translator, s.NewArrayVariable("same", s.ArrayLayout(s.ScalarType(s.BoolType))))
}
