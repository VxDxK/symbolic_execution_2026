package internal

import (
	"github.com/ebukreev/go-z3/z3"
	"math"
	"testing"
)

func TestAnalyseFloat64(t *testing.T) {
	source := `package fixture
func f(x float64) float64 { return -x / 0.0 }
func branch(x float64) bool { if x != x { return true }; return false }
`
	states := Analyse(source, "f")
	if len(states) != 1 || states[0].Status != Returned {
		t.Fatalf("unexpected states: %v", states)
	}
	state := states[0]
	translator := state.Analyser.Z3Translator
	ctx := translator.GetContext().(*z3.Context)
	result, err := translator.TranslateExpression(state.frame().ReturnValues[0])
	if err != nil {
		t.Fatal(err)
	}
	x := ctx.Const("x", ctx.FloatSort(11, 53)).(z3.Float)
	for _, input := range []float64{1, -1, 0, math.Copysign(0, -1), math.Inf(1), math.NaN()} {
		solver := z3.NewSolver(ctx)
		solver.Assert(x.Eq(ctx.FromFloat64(input, ctx.FloatSort(11, 53))))
		sat, err := solver.Check()
		if err != nil || !sat {
			t.Fatalf("model: %v", err)
		}
		value := result.(z3.Float)
		model := solver.Model()
		zero := float64(0)
		want := -input / zero
		nan, _ := model.Eval(value.IsNaN(), true).(z3.Bool).AsBool()
		if math.IsNaN(want) {
			if !nan {
				t.Fatal("expected NaN")
			}
			continue
		}
		bits, lit, valid := model.Eval(value.ToIEEEBV(), true).(z3.BV).AsUint64()
		if !lit || !valid || bits != math.Float64bits(want) {
			t.Fatalf("input %v: bits %x, want %x", input, bits, math.Float64bits(want))
		}
	}
	branches := Analyse(source, "branch")
	if len(branches) != 2 {
		t.Fatalf("NaN branch missing: %v", branches)
	}
	for _, state := range branches {
		if state.Status != Returned {
			t.Fatal(state.Status)
		}
	}
}

func TestAnalyseLoopVisitLimit(t *testing.T) {
	states := Analyse("package fixture\nfunc f(n int) int { i:=0; for i<n { i++ }; return i }", "f")
	returned, limited := 0, 0
	for _, state := range states {
		switch state.Status {
		case Returned:
			returned++
		case LimitReached:
			limited++
			if state.Diagnostic != "block visit limit reached" {
				t.Fatal(state.Diagnostic)
			}
		default:
			t.Fatal(state.Status)
		}
	}
	if returned < 2 || limited != 1 {
		t.Fatalf("returned=%d limited=%d", returned, limited)
	}
}
