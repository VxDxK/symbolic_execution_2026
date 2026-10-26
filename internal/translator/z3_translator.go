package translator

import (
	"fmt"

	"github.com/ebukreev/go-z3/z3"
	"symbolic-execution-course/internal/symbolic"
)

type cachedVariable struct {
	typeOf symbolic.ExpressionType
	value  z3.Value
}

type Z3Translator struct {
	ctx    *z3.Context
	config *z3.Config
	vars   map[string]cachedVariable
	err    error
}

func NewZ3Translator() *Z3Translator {
	config := z3.NewContextConfig()
	ctx := z3.NewContext(config)
	return &Z3Translator{ctx: ctx, config: config, vars: make(map[string]cachedVariable)}
}

func (zt *Z3Translator) GetContext() interface{} { return zt.ctx }

func (zt *Z3Translator) Reset() {
	zt.vars = make(map[string]cachedVariable)
	zt.err = nil
}

func (zt *Z3Translator) Close() {}

func (zt *Z3Translator) TranslateExpression(expr symbolic.SymbolicExpression) (interface{}, error) {
	zt.err = nil
	if expr == nil {
		return nil, NewTranslationError("cannot translate a nil expression", nil)
	}
	result := expr.Accept(zt)
	if zt.err != nil {
		return nil, zt.err
	}
	if result == nil {
		return nil, NewTranslationError("translation produced no Z3 value", expr)
	}
	return result, nil
}

func (zt *Z3Translator) VisitVariable(expr *symbolic.SymbolicVariable) interface{} {
	if cached, ok := zt.vars[expr.Name]; ok {
		if cached.typeOf != expr.Type() {
			return zt.fail(expr, "variable %q is already declared as %s, not %s", expr.Name, cached.typeOf, expr.Type())
		}
		return cached.value
	}
	value := zt.createZ3Variable(expr.Name, expr.Type())
	if value == nil {
		return zt.fail(expr, "unsupported variable type %s", expr.Type())
	}
	zt.vars[expr.Name] = cachedVariable{typeOf: expr.Type(), value: value}
	return value
}

func (zt *Z3Translator) VisitIntConstant(expr *symbolic.IntConstant) interface{} {
	if !expr.Type().IsInteger() {
		return zt.fail(expr, "integer constant has non-integer type %s", expr.Type())
	}
	return zt.ctx.FromBigInt(expr.BigIntValue(), zt.ctx.BVSort(expr.Type().BitWidth())).(z3.BV)
}

func (zt *Z3Translator) VisitBoolConstant(expr *symbolic.BoolConstant) interface{} {
	return zt.ctx.FromBool(expr.Value)
}

func (zt *Z3Translator) VisitBinaryOperation(expr *symbolic.BinaryOperation) interface{} {
	leftValue := expr.Left.Accept(zt)
	rightValue := expr.Right.Accept(zt)
	if zt.err != nil {
		return nil
	}

	if expr.Left.Type() == symbolic.BoolType {
		left, lok := leftValue.(z3.Bool)
		right, rok := rightValue.(z3.Bool)
		if !lok || !rok {
			return zt.fail(expr, "operator %s expected boolean operands", expr.Operator)
		}
		switch expr.Operator {
		case symbolic.EQ:
			return left.Eq(right)
		case symbolic.NE:
			return left.NE(right)
		default:
			return zt.fail(expr, "unsupported boolean binary operator %s", expr.Operator)
		}
	}

	left, lok := leftValue.(z3.BV)
	right, rok := rightValue.(z3.BV)
	if !lok || !rok {
		return zt.fail(expr, "operator %s expected bit-vector operands", expr.Operator)
	}
	signed := expr.Left.Type().IsSigned()
	switch expr.Operator {
	case symbolic.ADD:
		return left.Add(right)
	case symbolic.SUB:
		return left.Sub(right)
	case symbolic.MUL:
		return left.Mul(right)
	case symbolic.DIV:
		if signed {
			return left.SDiv(right)
		}
		return left.UDiv(right)
	case symbolic.MOD:
		if signed {
			return left.SRem(right)
		}
		return left.URem(right)
	case symbolic.BIT_AND:
		return left.And(right)
	case symbolic.BIT_OR:
		return left.Or(right)
	case symbolic.BIT_XOR:
		return left.Xor(right)
	case symbolic.SHL, symbolic.SHR:
		return zt.translateShift(expr, left, right)
	case symbolic.EQ:
		return left.Eq(right)
	case symbolic.NE:
		return left.NE(right)
	case symbolic.LT:
		if signed {
			return left.SLT(right)
		}
		return left.ULT(right)
	case symbolic.LE:
		if signed {
			return left.SLE(right)
		}
		return left.ULE(right)
	case symbolic.GT:
		if signed {
			return left.SGT(right)
		}
		return left.UGT(right)
	case symbolic.GE:
		if signed {
			return left.SGE(right)
		}
		return left.UGE(right)
	default:
		return zt.fail(expr, "unsupported binary operator %s", expr.Operator)
	}
}

func (zt *Z3Translator) VisitLogicalOperation(expr *symbolic.LogicalOperation) interface{} {
	operands := make([]z3.Bool, len(expr.Operands))
	for i, operand := range expr.Operands {
		value := operand.Accept(zt)
		if zt.err != nil {
			return nil
		}
		var ok bool
		operands[i], ok = value.(z3.Bool)
		if !ok {
			return zt.fail(expr, "operator %s expected boolean operands", expr.Operator)
		}
	}
	switch expr.Operator {
	case symbolic.AND:
		return operands[0].And(operands[1:]...)
	case symbolic.OR:
		return operands[0].Or(operands[1:]...)
	case symbolic.NOT:
		return operands[0].Not()
	case symbolic.IMPLIES:
		return operands[0].Implies(operands[1])
	default:
		return zt.fail(expr, "unsupported logical operator %s", expr.Operator)
	}
}

func (zt *Z3Translator) VisitUnaryOperation(expr *symbolic.UnaryOperation) interface{} {
	value, ok := expr.Operand.Accept(zt).(z3.BV)
	if zt.err != nil {
		return nil
	}
	if !ok {
		return zt.fail(expr, "unary operator %s expected a bit-vector", expr.Operator)
	}
	switch expr.Operator {
	case symbolic.NEG:
		return value.Neg()
	case symbolic.BIT_NOT:
		return value.Not()
	default:
		return zt.fail(expr, "unsupported unary operator %s", expr.Operator)
	}
}

func (zt *Z3Translator) VisitConversion(expr *symbolic.Conversion) interface{} {
	value, ok := expr.Operand.Accept(zt).(z3.BV)
	if zt.err != nil {
		return nil
	}
	if !ok {
		return zt.fail(expr, "integer conversion expected a bit-vector")
	}
	return resizeBV(value, expr.Operand.Type().IsSigned(), expr.TargetType.BitWidth())
}

func (zt *Z3Translator) createZ3Variable(name string, exprType symbolic.ExpressionType) z3.Value {
	if exprType.IsInteger() {
		return zt.ctx.BVConst(name, exprType.BitWidth())
	}
	if exprType == symbolic.BoolType {
		return zt.ctx.BoolConst(name)
	}
	return nil
}

func (zt *Z3Translator) castToZ3Type(value interface{}, targetType symbolic.ExpressionType) (z3.Value, error) {
	if targetType.IsInteger() {
		bv, ok := value.(z3.BV)
		if !ok || bv.Sort().BVSize() != targetType.BitWidth() {
			return nil, fmt.Errorf("expected a %d-bit bit-vector", targetType.BitWidth())
		}
		return bv, nil
	}
	if targetType == symbolic.BoolType {
		boolean, ok := value.(z3.Bool)
		if !ok {
			return nil, fmt.Errorf("expected a boolean Z3 value")
		}
		return boolean, nil
	}
	return nil, fmt.Errorf("unsupported target type %s", targetType)
}

func (zt *Z3Translator) translateShift(expr *symbolic.BinaryOperation, left, count z3.BV) z3.BV {
	leftWidth := left.Sort().BVSize()
	operationWidth := leftWidth
	if count.Sort().BVSize() > operationWidth {
		operationWidth = count.Sort().BVSize()
	}

	wideLeft := resizeBV(left, expr.Left.Type().IsSigned(), operationWidth)
	wideCount := resizeBV(count, false, operationWidth)
	var result z3.BV
	switch expr.Operator {
	case symbolic.SHL:
		result = wideLeft.Lsh(wideCount)
	case symbolic.SHR:
		if expr.Left.Type().IsSigned() {
			result = wideLeft.SRsh(wideCount)
		} else {
			result = wideLeft.URsh(wideCount)
		}
	}
	return resizeBV(result, expr.Left.Type().IsSigned(), leftWidth)
}

func resizeBV(value z3.BV, signed bool, targetWidth int) z3.BV {
	width := value.Sort().BVSize()
	switch {
	case width < targetWidth && signed:
		return value.SignExtend(targetWidth - width)
	case width < targetWidth:
		return value.ZeroExtend(targetWidth - width)
	case width > targetWidth:
		return value.Extract(targetWidth-1, 0)
	default:
		return value
	}
}

func (zt *Z3Translator) fail(expr symbolic.SymbolicExpression, format string, args ...interface{}) interface{} {
	if zt.err == nil {
		zt.err = NewTranslationError(fmt.Sprintf(format, args...), expr)
	}
	return nil
}
