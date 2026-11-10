// Package symbolic определяет базовые типы символьных выражений
package symbolic

import (
	"fmt"
	"strconv"
	"strings"
)

// ExpressionType представляет тип символьного выражения
type ExpressionType int

const (
	IntType ExpressionType = iota
	BoolType
	ArrayType
	UintType
	Int8Type
	Int16Type
	Int32Type
	Int64Type
	Uint8Type
	Uint16Type
	Uint32Type
	Uint64Type
	UintptrType
	RefType
	StructType
)

const (
	ByteType = Uint8Type
	RuneType = Int32Type
)

// String возвращает строковое представление типа
func (et ExpressionType) String() string {
	switch et {
	case IntType:
		return "int"
	case BoolType:
		return "bool"
	case ArrayType:
		return "array"
	case UintType:
		return "uint"
	case Int8Type:
		return "int8"
	case Int16Type:
		return "int16"
	case Int32Type:
		return "int32"
	case Int64Type:
		return "int64"
	case Uint8Type:
		return "uint8"
	case Uint16Type:
		return "uint16"
	case Uint32Type:
		return "uint32"
	case Uint64Type:
		return "uint64"
	case UintptrType:
		return "uintptr"
	case RefType:
		return "ref"
	case StructType:
		return "struct"
	default:
		return "unknown"
	}
}

func (et ExpressionType) IsInteger() bool {
	switch et {
	case IntType, UintType, Int8Type, Int16Type, Int32Type, Int64Type,
		Uint8Type, Uint16Type, Uint32Type, Uint64Type, UintptrType:
		return true
	default:
		return false
	}
}

func (et ExpressionType) BitWidth() int {
	switch et {
	case IntType, UintType, UintptrType:
		return strconv.IntSize
	case Int8Type, Uint8Type:
		return 8
	case Int16Type, Uint16Type:
		return 16
	case Int32Type, Uint32Type:
		return 32
	case Int64Type, Uint64Type:
		return 64
	default:
		return 0
	}
}

func (et ExpressionType) IsSigned() bool {
	switch et {
	case IntType, Int8Type, Int16Type, Int32Type, Int64Type:
		return true
	default:
		return false
	}
}

// TypeInfo is an immutable description of a value or allocated object.
type TypeInfo struct {
	kind                    ExpressionType
	domain, element, target *TypeInfo
	name                    string
	fields                  []*TypeInfo
}

func ScalarType(kind ExpressionType) *TypeInfo {
	if !kind.IsInteger() && kind != BoolType {
		panic("scalar type must be integer or boolean")
	}
	return &TypeInfo{kind: kind}
}

func ReferenceType(target *TypeInfo) *TypeInfo {
	requireType(target)
	return &TypeInfo{kind: RefType, target: target}
}

func ArraySort(domain, element *TypeInfo) *TypeInfo {
	requireType(domain)
	requireType(element)
	if domain.kind == StructType || element.kind == StructType {
		panic("SMT arrays require value sorts")
	}
	return &TypeInfo{kind: ArrayType, domain: domain, element: element}
}

func StructLayout(name string, fields ...*TypeInfo) *TypeInfo {
	if name == "" {
		panic("struct layout requires a name")
	}
	for _, field := range fields {
		requireType(field)
		if field.kind == StructType || field.kind == ArrayType {
			panic("aggregate fields must be represented by references")
		}
	}
	return &TypeInfo{kind: StructType, name: name, fields: append([]*TypeInfo(nil), fields...)}
}

func ArrayLayout(element *TypeInfo) *TypeInfo {
	return ArraySort(ScalarType(IntType), element)
}

func requireType(t *TypeInfo) {
	if t == nil {
		panic("type description must not be nil")
	}
}

func (t *TypeInfo) Kind() ExpressionType { return t.kind }
func (t *TypeInfo) Domain() *TypeInfo    { return t.domain }
func (t *TypeInfo) Element() *TypeInfo   { return t.element }
func (t *TypeInfo) Target() *TypeInfo    { return t.target }
func (t *TypeInfo) Name() string         { return t.name }
func (t *TypeInfo) Fields() []*TypeInfo  { return append([]*TypeInfo(nil), t.fields...) }
func (t *TypeInfo) Equal(other *TypeInfo) bool {
	return t != nil && other != nil && t.String() == other.String()
}
func (t *TypeInfo) String() string {
	switch t.kind {
	case RefType:
		return "ref(" + t.target.String() + ")"
	case ArrayType:
		return "array(" + t.domain.String() + "," + t.element.String() + ")"
	case StructType:
		fields := make([]string, len(t.fields))
		for i, field := range t.fields {
			fields[i] = field.String()
		}
		return fmt.Sprintf("struct(%q;%s)", t.name, strings.Join(fields, ","))
	default:
		return t.kind.String()
	}
}

func TypeOf(expr SymbolicExpression) *TypeInfo {
	if expr == nil {
		panic("expression must not be nil")
	}
	if typed, ok := expr.(interface{ ValueType() *TypeInfo }); ok {
		return typed.ValueType()
	}
	return ScalarType(expr.Type())
}
