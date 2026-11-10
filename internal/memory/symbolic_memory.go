package memory

import (
	"fmt"
	"math/big"
	"strconv"
	"sync/atomic"

	"symbolic-execution-course/internal/symbolic"
)

type Memory interface {
	Allocate(tpe symbolic.ExpressionType) *symbolic.Ref
	AllocateStruct(layout *symbolic.TypeInfo) *symbolic.Ref
	AllocateArray(element *symbolic.TypeInfo) *symbolic.Ref
	NewSymbolicRef(name string, target *symbolic.TypeInfo) *symbolic.Ref
	AssignField(ref *symbolic.Ref, fieldIdx int, value symbolic.SymbolicExpression)
	GetFieldValue(ref *symbolic.Ref, fieldIdx int) symbolic.SymbolicExpression
	AssignToArray(ref *symbolic.Ref, index int, value symbolic.SymbolicExpression)
	GetFromArray(ref *symbolic.Ref, index int) symbolic.SymbolicExpression
	AssignToSymbolicIndex(ref *symbolic.Ref, index, value symbolic.SymbolicExpression)
	GetFromSymbolicIndex(ref *symbolic.Ref, index symbolic.SymbolicExpression) symbolic.SymbolicExpression
	Constraints() symbolic.SymbolicExpression
	Clone() *SymbolicMemory
}

type memoryFamily struct {
	id          uint64
	nextAddress atomic.Uint64
}

var nextFamily atomic.Uint64

type SymbolicMemory struct {
	family    *memoryFamily
	regions   map[string]symbolic.SymbolicExpression
	layouts   map[string]*symbolic.TypeInfo
	allocated []*symbolic.Ref
	inputs    []*symbolic.Ref
}

func NewSymbolicMemory() SymbolicMemory {
	return SymbolicMemory{
		family:  &memoryFamily{id: nextFamily.Add(1)},
		regions: make(map[string]symbolic.SymbolicExpression),
		layouts: make(map[string]*symbolic.TypeInfo),
	}
}

func (mem *SymbolicMemory) Allocate(tpe symbolic.ExpressionType) *symbolic.Ref {
	if tpe == symbolic.ArrayType {
		return mem.AllocateArray(symbolic.ScalarType(symbolic.IntType))
	}
	return mem.allocate(symbolic.ScalarType(tpe))
}

func (mem *SymbolicMemory) AllocateStruct(layout *symbolic.TypeInfo) *symbolic.Ref {
	if layout == nil || layout.Kind() != symbolic.StructType {
		panic("memory: expected a struct layout")
	}
	return mem.allocate(layout)
}

func (mem *SymbolicMemory) AllocateArray(element *symbolic.TypeInfo) *symbolic.Ref {
	return mem.allocate(symbolic.ArrayLayout(element))
}

func (mem *SymbolicMemory) allocate(target *symbolic.TypeInfo) *symbolic.Ref {
	mem.register(target)
	id := mem.family.nextAddress.Add(1)
	if id == 0 || (strconv.IntSize == 32 && id > 1<<32-1) {
		panic("memory: address space exhausted")
	}
	address := symbolic.NewBigIntConstant(new(big.Int).SetUint64(id), symbolic.UintptrType)
	ref := symbolic.NewRef(address, target)
	if target.Kind() == symbolic.ArrayType {
		key := mem.arrayKey(target)
		heap := mem.region(key, target)
		mem.regions[key] = symbolic.NewArrayStore(heap, address, zeroValue(target))
	} else {
		for index, field := range fields(target) {
			key := mem.fieldKey(target, index)
			mem.regions[key] = symbolic.NewArrayStore(mem.region(key, field), address, zeroValue(field))
		}
	}
	mem.allocated = append(mem.allocated, ref)
	return ref
}

// NewSymbolicRef registers an incoming pointer for freshness constraints.
// Incoming pointers may alias; callers must guard symbolic dereferences against nil.
func (mem *SymbolicMemory) NewSymbolicRef(name string, target *symbolic.TypeInfo) *symbolic.Ref {
	mem.register(target)
	if name == "" {
		panic("memory: symbolic reference requires a name")
	}
	ref := symbolic.NewSymbolicRef(name, target)
	mem.inputs = append(mem.inputs, ref)
	return ref
}

func (mem *SymbolicMemory) AssignField(ref *symbolic.Ref, fieldIdx int, value symbolic.SymbolicExpression) {
	field := mem.field(ref, fieldIdx)
	requireValue(field, value)
	key := mem.fieldKey(ref.Target(), fieldIdx)
	mem.regions[key] = symbolic.NewArrayStore(mem.region(key, field), ref.Address(), value)
}

func (mem *SymbolicMemory) GetFieldValue(ref *symbolic.Ref, fieldIdx int) symbolic.SymbolicExpression {
	field := mem.field(ref, fieldIdx)
	return symbolic.NewArraySelect(mem.region(mem.fieldKey(ref.Target(), fieldIdx), field), ref.Address())
}

func (mem *SymbolicMemory) AssignToArray(ref *symbolic.Ref, index int, value symbolic.SymbolicExpression) {
	mem.AssignToSymbolicIndex(ref, symbolic.NewIntConstant(int64(index)), value)
}

func (mem *SymbolicMemory) GetFromArray(ref *symbolic.Ref, index int) symbolic.SymbolicExpression {
	return mem.GetFromSymbolicIndex(ref, symbolic.NewIntConstant(int64(index)))
}

func (mem *SymbolicMemory) AssignToSymbolicIndex(ref *symbolic.Ref, index, value symbolic.SymbolicExpression) {
	target := mem.array(ref, index)
	requireValue(target.Element(), value)
	key := mem.arrayKey(target)
	heap := mem.region(key, target)
	array := symbolic.NewArraySelect(heap, ref.Address())
	updated := symbolic.NewArrayStore(array, index, value)
	mem.regions[key] = symbolic.NewArrayStore(heap, ref.Address(), updated)
}

func (mem *SymbolicMemory) GetFromSymbolicIndex(ref *symbolic.Ref, index symbolic.SymbolicExpression) symbolic.SymbolicExpression {
	target := mem.array(ref, index)
	array := symbolic.NewArraySelect(mem.region(mem.arrayKey(target), target), ref.Address())
	return symbolic.NewArraySelect(array, index)
}

// Constraints must be obtained after all incoming references and allocations are declared.
func (mem *SymbolicMemory) Constraints() symbolic.SymbolicExpression {
	conditions := []symbolic.SymbolicExpression{symbolic.NewBoolConstant(true)}
	for _, fresh := range mem.allocated {
		for _, input := range mem.inputs {
			conditions = append(conditions, symbolic.NewBinaryOperation(fresh.Address(), input.Address(), symbolic.NE))
		}
	}
	if len(conditions) == 1 {
		return conditions[0]
	}
	return symbolic.NewLogicalOperation(conditions, symbolic.AND)
}

func (mem *SymbolicMemory) Clone() *SymbolicMemory {
	result := *mem
	result.regions = make(map[string]symbolic.SymbolicExpression, len(mem.regions))
	for key, value := range mem.regions {
		result.regions[key] = value
	}
	result.layouts = make(map[string]*symbolic.TypeInfo, len(mem.layouts))
	for key, value := range mem.layouts {
		result.layouts[key] = value
	}
	result.allocated = append([]*symbolic.Ref(nil), mem.allocated...)
	result.inputs = append([]*symbolic.Ref(nil), mem.inputs...)
	return &result
}

func (mem *SymbolicMemory) region(key string, valueType *symbolic.TypeInfo) symbolic.SymbolicExpression {
	if region, ok := mem.regions[key]; ok {
		return region
	}
	name := fmt.Sprintf("@memory.%d.%s", mem.family.id, key)
	region := symbolic.NewArrayVariable(name, symbolic.ArraySort(symbolic.ScalarType(symbolic.UintptrType), valueType))
	mem.regions[key] = region
	return region
}

func (mem *SymbolicMemory) fieldKey(target *symbolic.TypeInfo, index int) string {
	return fmt.Sprintf("field.%s.%d", target, index)
}
func (mem *SymbolicMemory) arrayKey(target *symbolic.TypeInfo) string {
	return "elements." + target.String()
}

func (mem *SymbolicMemory) validate(ref *symbolic.Ref) {
	if ref == nil {
		panic("memory: nil reference")
	}
	if constant, ok := ref.Address().(*symbolic.IntConstant); ok {
		address := constant.BigIntValue()
		address.Mod(address, new(big.Int).Lsh(big.NewInt(1), uint(symbolic.UintptrType.BitWidth())))
		if address.Sign() == 0 {
			panic("memory: nil dereference")
		}
	}
	mem.register(ref.Target())
}

func (mem *SymbolicMemory) field(ref *symbolic.Ref, index int) *symbolic.TypeInfo {
	mem.validate(ref)
	if ref.Target().Kind() == symbolic.ArrayType {
		panic("memory: array is not a struct or primitive cell")
	}
	types := fields(ref.Target())
	if index < 0 || index >= len(types) {
		panic("memory: field index out of range")
	}
	return types[index]
}

func (mem *SymbolicMemory) array(ref *symbolic.Ref, index symbolic.SymbolicExpression) *symbolic.TypeInfo {
	mem.validate(ref)
	if ref.Target().Kind() != symbolic.ArrayType {
		panic("memory: expected array reference")
	}
	if index == nil || !symbolic.TypeOf(index).Equal(ref.Target().Domain()) {
		panic("memory: array index must have int type")
	}
	return ref.Target()
}

func (mem *SymbolicMemory) register(target *symbolic.TypeInfo) {
	if target == nil {
		panic("memory: missing target type")
	}
	switch target.Kind() {
	case symbolic.StructType:
		if previous, ok := mem.layouts[target.Name()]; ok && !previous.Equal(target) {
			panic("memory: conflicting struct layout " + target.Name())
		}
		mem.layouts[target.Name()] = target
		for _, field := range target.Fields() {
			mem.register(field)
		}
	case symbolic.ArrayType:
		if !target.Domain().Equal(symbolic.ScalarType(symbolic.IntType)) {
			panic("memory: array object indices must have int type")
		}
		mem.register(target.Element())
	case symbolic.RefType:
		mem.register(target.Target())
	default:
		if !target.Kind().IsInteger() && target.Kind() != symbolic.BoolType {
			panic("memory: unsupported object type")
		}
	}
}

func fields(target *symbolic.TypeInfo) []*symbolic.TypeInfo {
	if target.Kind() == symbolic.StructType {
		return target.Fields()
	}
	return []*symbolic.TypeInfo{target}
}

func requireValue(expected *symbolic.TypeInfo, value symbolic.SymbolicExpression) {
	if value == nil || !expected.Equal(symbolic.TypeOf(value)) {
		panic("memory: value type mismatch")
	}
}

func zeroValue(typ *symbolic.TypeInfo) symbolic.SymbolicExpression {
	switch typ.Kind() {
	case symbolic.BoolType:
		return symbolic.NewBoolConstant(false)
	case symbolic.RefType:
		return symbolic.NewNilRef(typ.Target())
	case symbolic.ArrayType:
		return symbolic.NewConstantArray(typ.Domain(), zeroValue(typ.Element()))
	default:
		return symbolic.NewTypedIntConstant(0, typ.Kind())
	}
}

var _ Memory = (*SymbolicMemory)(nil)
