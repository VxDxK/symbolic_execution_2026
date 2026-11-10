package memory_test

import (
	"testing"

	"github.com/ebukreev/go-z3/z3"
	"symbolic-execution-course/internal/memory"
	s "symbolic-execution-course/internal/symbolic"
	"symbolic-execution-course/internal/translator"
)

func newTranslator(t *testing.T) *translator.Z3Translator {
	t.Helper()
	tr := translator.NewZ3Translator()
	t.Cleanup(tr.Close)
	return tr
}

func translate(t *testing.T, tr *translator.Z3Translator, expr s.SymbolicExpression) z3.Value {
	t.Helper()
	value, err := tr.TranslateExpression(expr)
	if err != nil {
		t.Fatal(err)
	}
	return value.(z3.Value)
}

func solve(t *testing.T, tr *translator.Z3Translator, want bool, conditions ...s.SymbolicExpression) *z3.Model {
	t.Helper()
	solver := z3.NewSolver(tr.GetContext().(*z3.Context))
	for _, condition := range conditions {
		solver.Assert(translate(t, tr, condition).(z3.Bool))
	}
	sat, err := solver.Check()
	if err != nil {
		t.Fatal(err)
	}
	if sat != want {
		t.Fatalf("SAT = %t, want %t; formula: %s", sat, want, solver)
	}
	if !sat {
		return nil
	}
	return solver.Model()
}

func equal(left, right s.SymbolicExpression) s.SymbolicExpression {
	return s.NewBinaryOperation(left, right, s.EQ)
}
func different(left, right s.SymbolicExpression) s.SymbolicExpression {
	return s.NewBinaryOperation(left, right, s.NE)
}
func nonnil(ref *s.Ref) s.SymbolicExpression { return different(ref, s.NewNilRef(ref.Target())) }
func proveEqual(t *testing.T, tr *translator.Z3Translator, left, right s.SymbolicExpression) {
	t.Helper()
	solve(t, tr, false, different(left, right))
}

func TestTypedMemoryAndInitialization(t *testing.T) {
	tr := newTranslator(t)
	types := []s.ExpressionType{s.IntType, s.UintType, s.Int8Type, s.Int16Type, s.Int32Type, s.Int64Type, s.Uint8Type, s.Uint16Type, s.Uint32Type, s.Uint64Type, s.UintptrType, s.BoolType}
	for _, typ := range types {
		t.Run(typ.String(), func(t *testing.T) {
			mem := memory.NewSymbolicMemory()
			desc := s.ScalarType(typ)
			layout := s.StructLayout("Typed", desc, desc)
			object, other := mem.AllocateStruct(layout), mem.AllocateStruct(layout)
			array := mem.AllocateArray(desc)
			primitive := mem.Allocate(typ)
			var zero, value s.SymbolicExpression
			if typ == s.BoolType {
				zero, value = s.NewBoolConstant(false), s.NewBoolConstant(true)
			} else {
				zero, value = s.NewTypedIntConstant(0, typ), s.NewTypedIntConstant(7, typ)
			}
			proveEqual(t, tr, mem.GetFieldValue(object, 0), zero)
			proveEqual(t, tr, mem.GetFromArray(array, 10), zero)
			proveEqual(t, tr, mem.GetFieldValue(primitive, 0), zero)
			mem.AssignField(object, 0, value)
			mem.AssignToArray(array, 5, value)
			mem.AssignField(primitive, 0, value)
			proveEqual(t, tr, mem.GetFieldValue(object, 0), value)
			proveEqual(t, tr, mem.GetFieldValue(object, 1), zero)
			proveEqual(t, tr, mem.GetFieldValue(other, 0), zero)
			proveEqual(t, tr, mem.GetFromArray(array, 5), value)
			proveEqual(t, tr, mem.GetFromArray(array, 10), zero)
			proveEqual(t, tr, mem.GetFieldValue(primitive, 0), value)
			mem.AssignField(object, 0, zero)
			mem.AssignToArray(array, 5, zero)
			proveEqual(t, tr, mem.GetFieldValue(object, 0), zero)
			proveEqual(t, tr, mem.GetFromArray(array, 5), zero)
		})
	}
	mem := memory.NewSymbolicMemory()
	array := mem.Allocate(s.ArrayType)
	mem.AssignToArray(array, 5, s.NewIntConstant(10))
	proveEqual(t, tr, mem.GetFromArray(array, 5), s.NewIntConstant(10))
	proveEqual(t, tr, mem.GetFromArray(array, 10), s.NewIntConstant(0))
}

func TestSolverDiscoversStructAliasing(t *testing.T) {
	mem := memory.NewSymbolicMemory()
	tr := newTranslator(t)
	layout := s.StructLayout("Foo", s.ScalarType(s.IntType))
	foo1, foo2 := mem.NewSymbolicRef("foo1", layout), mem.NewSymbolicRef("foo2", layout)
	old := mem.GetFieldValue(foo2, 0)
	// Incoming memory is unknown rather than implicitly zero-initialized.
	solve(t, tr, true, nonnil(foo2), equal(old, s.NewIntConstant(19)))
	solve(t, tr, true, nonnil(foo2), equal(old, s.NewIntConstant(23)))
	mem.AssignField(foo2, 0, s.NewIntConstant(5))
	mem.AssignField(foo1, 0, s.NewIntConstant(2))
	result := mem.GetFieldValue(foo2, 0)
	condition := equal(result, s.NewIntConstant(2))
	model := solve(t, tr, true, mem.Constraints(), nonnil(foo1), nonnil(foo2), condition)
	addressesEqual := model.Eval(translate(t, tr, equal(foo1, foo2)), true).(z3.Bool)
	if value, ok := addressesEqual.AsBool(); !ok || !value {
		t.Fatal("model did not discover equal addresses")
	}
	solve(t, tr, false, condition, different(foo1, foo2))
	solve(t, tr, false, equal(foo1, foo2), different(result, s.NewIntConstant(2)))
	solve(t, tr, false, different(foo1, foo2), different(result, s.NewIntConstant(5)))

	copyOfFoo1 := s.NewRef(foo1.Address(), layout)
	if copyOfFoo1 == foo1 {
		t.Fatal("expected distinct Go reference objects")
	}
	mem.AssignField(copyOfFoo1, 0, s.NewIntConstant(31))
	proveEqual(t, tr, copyOfFoo1, foo1)
	proveEqual(t, tr, mem.GetFieldValue(foo1, 0), s.NewIntConstant(31))
}

func TestSolverDiscoversArrayAliasing(t *testing.T) {
	mem := memory.NewSymbolicMemory()
	tr := newTranslator(t)
	layout := s.ArrayLayout(s.ScalarType(s.IntType))
	a, b := mem.NewSymbolicRef("a", layout), mem.NewSymbolicRef("b", layout)
	i, j := s.NewSymbolicVariable("i", s.IntType), s.NewSymbolicVariable("j", s.IntType)
	mem.AssignToSymbolicIndex(b, j, s.NewIntConstant(5))
	mem.AssignToSymbolicIndex(a, i, s.NewIntConstant(2))
	value := mem.GetFromSymbolicIndex(b, j)
	condition := equal(value, s.NewIntConstant(2))
	model := solve(t, tr, true, nonnil(a), nonnil(b), condition)
	for _, relation := range []s.SymbolicExpression{equal(a, b), equal(i, j)} {
		boolean := model.Eval(translate(t, tr, relation), true).(z3.Bool)
		if literal, ok := boolean.AsBool(); !ok || !literal {
			t.Fatal("model did not discover aliasing addresses and indices")
		}
	}
	solve(t, tr, false, condition, different(a, b))
	solve(t, tr, false, condition, different(i, j))
	solve(t, tr, false, equal(a, b), equal(i, j), different(value, s.NewIntConstant(2)))
	solve(t, tr, false, different(i, j), different(value, s.NewIntConstant(5)))
}

func TestReferencesReadFromMemory(t *testing.T) {
	mem := memory.NewSymbolicMemory()
	tr := newTranslator(t)
	leaf := s.StructLayout("Leaf", s.ScalarType(s.Int16Type))
	parentType := s.StructLayout("Parent", s.ReferenceType(leaf))
	parent, child := mem.AllocateStruct(parentType), mem.AllocateStruct(leaf)
	proveEqual(t, tr, mem.GetFieldValue(parent, 0), s.NewNilRef(leaf))
	mem.AssignField(parent, 0, child)
	array := mem.AllocateArray(s.ReferenceType(leaf))
	mem.AssignToArray(array, 1, child)
	for _, loaded := range []s.SymbolicExpression{mem.GetFieldValue(parent, 0), mem.GetFromArray(array, 1)} {
		ref, ok := loaded.(*s.Ref)
		if !ok || !ref.Target().Equal(leaf) {
			t.Fatal("loaded reference lost target metadata")
		}
		proveEqual(t, tr, ref, child)
		mem.AssignField(ref, 0, s.NewTypedIntConstant(17, s.Int16Type))
		proveEqual(t, tr, mem.GetFieldValue(child, 0), s.NewTypedIntConstant(17, s.Int16Type))
	}
	proveEqual(t, tr, mem.GetFromArray(array, 0), s.NewNilRef(leaf))

	ints := s.ArrayLayout(s.ScalarType(s.IntType))
	holder := mem.AllocateStruct(s.StructLayout("ArrayHolder", s.ReferenceType(ints)))
	numbers := mem.Allocate(s.ArrayType)
	mem.AssignField(holder, 0, numbers)
	read := mem.GetFieldValue(holder, 0).(*s.Ref)
	mem.AssignToArray(read, 7, s.NewIntConstant(99))
	proveEqual(t, tr, mem.GetFromArray(numbers, 7), s.NewIntConstant(99))
}

func TestPathConditionsAndTranslationAreImmutable(t *testing.T) {
	mem := memory.NewSymbolicMemory()
	tr := newTranslator(t)
	person := mem.NewSymbolicRef("person", s.StructLayout("Person", s.ScalarType(s.IntType)))
	old := mem.GetFieldValue(person, 0)
	oldCondition := different(old, s.NewIntConstant(18))
	before := translate(t, tr, oldCondition)
	mem.AssignField(person, 0, s.NewIntConstant(18))
	current := mem.GetFieldValue(person, 0)
	solve(t, tr, true, nonnil(person), oldCondition)
	solve(t, tr, false, nonnil(person), oldCondition, different(current, s.NewIntConstant(18)))
	translate(t, tr, current)
	after := translate(t, tr, oldCondition)
	if before.String() != after.String() {
		t.Fatal("translation changed after a write")
	}
	tr.Reset()
	translate(t, tr, current)
	if reversed := translate(t, tr, oldCondition); reversed.String() != before.String() {
		t.Fatal("translation depends on expression order")
	}
}

func TestClonesAreIndependentWithSharedTranslator(t *testing.T) {
	mem := memory.NewSymbolicMemory()
	tr := newTranslator(t)
	layout := s.StructLayout("Branch", s.ScalarType(s.IntType))
	object := mem.AllocateStruct(layout)
	array := mem.Allocate(s.ArrayType)
	left, right := mem.Clone(), mem.Clone()
	left.AssignField(object, 0, s.NewIntConstant(11))
	right.AssignField(object, 0, s.NewIntConstant(22))
	left.AssignToArray(array, 0, s.NewIntConstant(33))
	right.AssignToArray(array, 0, s.NewIntConstant(44))
	for _, branch := range []struct {
		mem            *memory.SymbolicMemory
		field, element int64
	}{{&mem, 0, 0}, {left, 11, 33}, {right, 22, 44}} {
		proveEqual(t, tr, branch.mem.GetFieldValue(object, 0), s.NewIntConstant(branch.field))
		proveEqual(t, tr, branch.mem.GetFromArray(array, 0), s.NewIntConstant(branch.element))
	}
	first, second := left.AllocateStruct(layout), right.AllocateStruct(layout)
	solve(t, tr, false, equal(first, second))

	// A region first accessed after cloning must still share the same incoming contents.
	input := mem.NewSymbolicRef("lateRegion", s.StructLayout("Late", s.ScalarType(s.BoolType)))
	left, right = mem.Clone(), mem.Clone()
	proveEqual(t, tr, left.GetFieldValue(input, 0), right.GetFieldValue(input, 0))
	left.AssignField(input, 0, s.NewBoolConstant(true))
	solve(t, tr, true, nonnil(input), equal(right.GetFieldValue(input, 0), s.NewBoolConstant(false)))
}

func TestFreshnessRegardlessOfDeclarationOrder(t *testing.T) {
	for _, inputFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "allocation first", true: "input first"}[inputFirst], func(t *testing.T) {
			mem := memory.NewSymbolicMemory()
			tr := newTranslator(t)
			layout := s.StructLayout("Fresh", s.ScalarType(s.IntType))
			var input *s.Ref
			if inputFirst {
				input = mem.NewSymbolicRef("input", layout)
			}
			fresh := mem.AllocateStruct(layout)
			if !inputFirst {
				input = mem.NewSymbolicRef("input", layout)
			}
			solve(t, tr, true, mem.Constraints(), nonnil(input))
			solve(t, tr, false, mem.Constraints(), equal(fresh, input))
			solve(t, tr, false, equal(fresh, s.NewNilRef(layout)))
			other := mem.AllocateStruct(layout)
			solve(t, tr, false, equal(fresh, other))
			// Sibling declarations must not mutate this state's constraints.
			sibling := mem.Clone()
			extra := sibling.NewSymbolicRef("extra", layout)
			solve(t, tr, true, mem.Constraints(), equal(extra, fresh))
			solve(t, tr, false, sibling.Constraints(), equal(extra, fresh))
		})
	}
}

func TestMemoryValidation(t *testing.T) {
	integer := s.ScalarType(s.IntType)
	layout := s.StructLayout("Validated", integer)
	for name, action := range map[string]func(){
		"nil reference":   func() { mem := memory.NewSymbolicMemory(); mem.GetFieldValue(nil, 0) },
		"nil dereference": func() { mem := memory.NewSymbolicMemory(); mem.GetFieldValue(s.NewNilRef(layout), 0) },
		"wrapped nil address": func() {
			mem := memory.NewSymbolicMemory()
			ref := s.NewRef(s.NewTypedIntConstant(0, s.UintptrType), layout)
			mem.AssignField(ref, 0, s.NewIntConstant(1))
		},
		"negative field": func() { mem := memory.NewSymbolicMemory(); mem.GetFieldValue(mem.AllocateStruct(layout), -1) },
		"missing field":  func() { mem := memory.NewSymbolicMemory(); mem.GetFieldValue(mem.AllocateStruct(layout), 1) },
		"wrong value": func() {
			mem := memory.NewSymbolicMemory()
			mem.AssignField(mem.AllocateStruct(layout), 0, s.NewBoolConstant(true))
		},
		"nil value":       func() { mem := memory.NewSymbolicMemory(); mem.AssignField(mem.AllocateStruct(layout), 0, nil) },
		"array as struct": func() { mem := memory.NewSymbolicMemory(); mem.GetFieldValue(mem.Allocate(s.ArrayType), 0) },
		"struct as array": func() { mem := memory.NewSymbolicMemory(); mem.GetFromArray(mem.AllocateStruct(layout), 0) },
		"wrong index": func() {
			mem := memory.NewSymbolicMemory()
			mem.GetFromSymbolicIndex(mem.Allocate(s.ArrayType), s.NewTypedIntConstant(0, s.UintType))
		},
		"wrong element": func() {
			mem := memory.NewSymbolicMemory()
			mem.AssignToArray(mem.Allocate(s.ArrayType), 0, s.NewBoolConstant(true))
		},
		"conflicting layout": func() {
			mem := memory.NewSymbolicMemory()
			mem.AllocateStruct(layout)
			mem.AllocateStruct(s.StructLayout("Validated", s.ScalarType(s.BoolType)))
		},
		"missing layout": func() { mem := memory.NewSymbolicMemory(); mem.AllocateStruct(nil) },
		"untyped struct": func() { mem := memory.NewSymbolicMemory(); mem.Allocate(s.StructType) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected validation panic")
				}
			}()
			action()
		})
	}
}
