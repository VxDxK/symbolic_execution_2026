package symbolic

// Visitor интерфейс для обхода символьных выражений (Visitor Pattern)
type Visitor interface {
	VisitVariable(expr *SymbolicVariable) interface{}
	VisitIntConstant(expr *IntConstant) interface{}
	VisitBoolConstant(expr *BoolConstant) interface{}
	VisitBinaryOperation(expr *BinaryOperation) interface{}
	VisitLogicalOperation(expr *LogicalOperation) interface{}
	VisitUnaryOperation(expr *UnaryOperation) interface{}
	VisitConversion(expr *Conversion) interface{}
	VisitRef(expr *Ref) interface{}
	VisitArrayVariable(expr *ArrayVariable) interface{}
	VisitConstantArray(expr *ConstantArray) interface{}
	VisitArraySelect(expr *ArraySelect) interface{}
	VisitArrayStore(expr *ArrayStore) interface{}
}
