// Package symbolic определяет базовые типы символьных выражений
package symbolic

import "strconv"

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
