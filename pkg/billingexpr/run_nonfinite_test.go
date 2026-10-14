package billingexpr_test

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

// A zero divisor makes expr-lang return +Inf (or NaN for 0/0) instead of
// failing. Converting that to an int yields an implementation-defined value
// (MinInt64 on amd64), which would settle as an enormous negative quota and
// refund the user. The run must fail instead of producing a billing number.
func TestRunExprRejectsNonFiniteResult(t *testing.T) {
	cases := []struct {
		name string
		expr string
		p    float64
		c    float64
	}{
		{name: "division_by_zero", expr: "p * 100 / c", p: 100, c: 0},
		{name: "zero_over_zero", expr: "p / c", p: 0, c: 0},
		{name: "overflow", expr: "p * 1e308 * 10", p: 1e10, c: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := billingexpr.RunExpr(tc.expr, billingexpr.TokenParams{P: tc.p, C: tc.c})
			if err == nil {
				t.Fatalf("RunExpr(%q) = %v, want error for a non-finite result", tc.expr, got)
			}
			if got != 0 {
				t.Fatalf("RunExpr(%q) = %v on error, want 0", tc.expr, got)
			}
		})
	}
}

// The finite path must be unaffected.
func TestRunExprKeepsFiniteResult(t *testing.T) {
	got, _, err := billingexpr.RunExpr("p * 100 / c", billingexpr.TokenParams{P: 100, C: 4})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(got-2500) > 1e-9 {
		t.Fatalf("got %v, want 2500", got)
	}
}
