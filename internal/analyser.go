package internal

import (
	"container/heap"
	"fmt"

	"github.com/ebukreev/go-z3/z3"
	"golang.org/x/tools/go/ssa"
	ssabuilder "symbolic-execution-course/internal/ssa"
	"symbolic-execution-course/internal/symbolic"
	"symbolic-execution-course/internal/translator"
)

const maxAnalysisSteps = 10000
const solverTimeoutMilliseconds = 1000

type Analyser struct {
	Package      *ssa.Package
	StatesQueue  PriorityQueue
	PathSelector PathSelector
	Results      []Interpreter
	Z3Translator *translator.Z3Translator
	solver       *z3.Solver
}

func Analyse(source string, functionName string) []Interpreter {
	function, err := ssabuilder.NewBuilder().ParseAndBuildSSA(source, functionName)
	if err != nil {
		panic(fmt.Sprintf("analysis: %v", err))
	}
	if len(function.Blocks) == 0 {
		panic("analysis: function has no body")
	}
	a := &Analyser{Package: function.Pkg, PathSelector: &BfsPathSelector{}, Z3Translator: translator.NewZ3Translator()}
	ctx := a.Z3Translator.GetContext().(*z3.Context)
	ctx.Config().SetUint("timeout", solverTimeoutMilliseconds)
	a.solver = z3.NewSolver(ctx)
	state := Interpreter{
		Analyser:      a,
		PathCondition: symbolic.NewBoolConstant(true),
		pathFormula:   ctx.FromBool(true),
		CallStack: []CallStackFrame{{
			Function:     function,
			CurrentBlock: function.Blocks[0],
			LocalMemory:  make(map[string]symbolic.SymbolicExpression),
		}},
	}
	for _, param := range function.Params {
		state.frame().LocalMemory[param.Name()] = symbolic.NewSymbolicVariable(param.Name(), expressionType(param.Type()))
	}
	for i := 0; i < function.Signature.Results().Len(); i++ {
		expressionType(function.Signature.Results().At(i).Type())
	}
	a.enqueue(state)
	for steps := 0; a.StatesQueue.Len() > 0 && steps < maxAnalysisSteps; steps++ {
		current := heap.Pop(&a.StatesQueue).(*Item).value
		frame := current.frame()
		if frame.InstructionIndex >= len(frame.CurrentBlock.Instrs) {
			panic("analysis: block ended without a terminator")
		}
		for _, next := range current.interpretDynamically(frame.CurrentBlock.Instrs[frame.InstructionIndex]) {
			if next.Status == Running {
				a.enqueue(next)
			} else {
				a.Results = append(a.Results, next)
			}
		}
	}
	for a.StatesQueue.Len() > 0 {
		state := heap.Pop(&a.StatesQueue).(*Item).value
		state.Status = LimitReached
		state.Diagnostic = "global instruction limit reached"
		a.Results = append(a.Results, state)
	}
	return a.Results
}

func (a *Analyser) enqueue(state Interpreter) {
	heap.Push(&a.StatesQueue, &Item{value: state, priority: a.PathSelector.CalculatePriority(state)})
}

func (a *Analyser) feasible(condition z3.Bool) bool {
	a.solver.Push()
	defer a.solver.Pop()
	a.solver.Assert(condition)
	sat, err := a.solver.Check()
	if err != nil {
		panic(fmt.Sprintf("analysis: solver could not decide path: %v", err))
	}
	return sat
}
