package billing_setting

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

var (
	mu sync.RWMutex

	// model -> "ratio" | "tiered_expr"
	billingModeMap = make(map[string]string)

	// model -> expr string (authored by frontend, stored directly)
	billingExprMap = make(map[string]string)
)

const (
	BillingModeRatio      = "ratio"
	BillingModeTieredExpr = "tiered_expr"
)

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

func GetBillingMode(model string) string {
	mu.RLock()
	defer mu.RUnlock()
	if mode, ok := billingModeMap[model]; ok {
		return mode
	}
	return BillingModeRatio
}

func GetBillingExpr(model string) (string, bool) {
	mu.RLock()
	defer mu.RUnlock()
	expr, ok := billingExprMap[model]
	return expr, ok
}

func UpdateBillingModeByJSONString(jsonStr string) error {
	var m map[string]string
	if err := common.Unmarshal([]byte(jsonStr), &m); err != nil {
		return fmt.Errorf("parse ModelBillingMode: %w", err)
	}
	for k, v := range m {
		if v != BillingModeRatio && v != BillingModeTieredExpr {
			return fmt.Errorf("invalid billing mode %q for model %q", v, k)
		}
	}
	mu.Lock()
	billingModeMap = m
	mu.Unlock()
	return nil
}

func UpdateBillingExprByJSONString(jsonStr string) error {
	var m map[string]string
	if err := common.Unmarshal([]byte(jsonStr), &m); err != nil {
		return fmt.Errorf("parse ModelBillingExpr: %w", err)
	}
	for model, exprStr := range m {
		if _, err := billingexpr.CompileFromCache(exprStr); err != nil {
			return fmt.Errorf("model %q: %w", model, err)
		}
		if err := smokeTestExpr(exprStr); err != nil {
			return fmt.Errorf("model %q smoke test: %w", model, err)
		}
	}
	mu.Lock()
	billingExprMap = m
	mu.Unlock()
	billingexpr.InvalidateCache()
	return nil
}

// ---------------------------------------------------------------------------
// JSON serializers (for OptionMap / API response)
// ---------------------------------------------------------------------------

func BillingMode2JSONString() string {
	mu.RLock()
	defer mu.RUnlock()
	b, err := common.Marshal(billingModeMap)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func BillingExpr2JSONString() string {
	mu.RLock()
	defer mu.RUnlock()
	b, err := common.Marshal(billingExprMap)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func smokeTestExpr(exprStr string) error {
	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0},
		{P: 1000, C: 1000},
		{P: 100000, C: 100000},
		{P: 1000000, C: 1000000},
	}
	requests := []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}

	for _, v := range vectors {
		for _, request := range requests {
			result, _, err := billingexpr.RunExprWithRequest(exprStr, v, request)
			if err != nil {
				return fmt.Errorf("vector {p=%g, c=%g}: run failed: %w", v.P, v.C, err)
			}
			if result < 0 {
				return fmt.Errorf("vector {p=%g, c=%g}: result %f < 0", v.P, v.C, result)
			}
		}
	}
	return nil
}
