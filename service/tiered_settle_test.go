package service

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// Claude Sonnet-style tiered expression: standard vs long-context
const sonnetTieredExpr = `p <= 200000 ? tier("standard", p * 1.5 + c * 7.5) : tier("long_context", p * 3 + c * 11.25)`

// Simple flat expression
const flatExpr = `tier("default", p * 2 + c * 10)`

// Expression with cache tokens
const cacheExpr = `tier("default", p * 2 + c * 10 + cr * 0.2 + cc * 2.5 + cc1h * 4)`

// Expression with request probes
const probeExpr = `param("service_tier") == "fast" ? tier("fast", p * 4 + c * 20) : tier("normal", p * 2 + c * 10)`

const testQuotaPerUnit = 500_000.0

func makeSnapshot(expr string, groupRatio float64, estPrompt, estCompletion int) *billingexpr.BillingSnapshot {
	return &billingexpr.BillingSnapshot{
		BillingMode:               "tiered_expr",
		ExprString:                expr,
		ExprHash:                  billingexpr.ExprHashString(expr),
		GroupRatio:                groupRatio,
		EstimatedPromptTokens:     estPrompt,
		EstimatedCompletionTokens: estCompletion,
		QuotaPerUnit:              testQuotaPerUnit,
	}
}

func makeRelayInfo(expr string, groupRatio float64, estPrompt, estCompletion int) *relaycommon.RelayInfo {
	snap := makeSnapshot(expr, groupRatio, estPrompt, estCompletion)
	cost, trace, _ := billingexpr.RunExpr(expr, billingexpr.TokenParams{P: float64(estPrompt), C: float64(estCompletion)})
	quotaBeforeGroup := cost / 1_000_000 * testQuotaPerUnit
	snap.EstimatedQuotaBeforeGroup = quotaBeforeGroup
	snap.EstimatedQuotaAfterGroup = billingexpr.QuotaRound(quotaBeforeGroup * groupRatio)
	snap.EstimatedTier = trace.MatchedTier
	return &relaycommon.RelayInfo{
		TieredBillingSnapshot: snap,
		FinalPreConsumedQuota: snap.EstimatedQuotaAfterGroup,
	}
}

// ---------------------------------------------------------------------------
// Existing tests (preserved)
// ---------------------------------------------------------------------------

func TestTryTieredSettleUsesFrozenRequestInput(t *testing.T) {
	exprStr := `param("service_tier") == "fast" ? tier("fast", p * 2) : tier("normal", p)`
	relayInfo := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                exprStr,
			ExprHash:                  billingexpr.ExprHashString(exprStr),
			GroupRatio:                1.0,
			EstimatedPromptTokens:     100,
			EstimatedCompletionTokens: 0,
			EstimatedQuotaAfterGroup:  50,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"service_tier":"fast"}`),
		},
	}

	ok, quota, result := TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 100})
	if !ok {
		t.Fatal("expected tiered settle to apply")
	}
	// fast: p*2 = 200; quota = 200 / 1M * 500K = 100
	if quota != 100 {
		t.Fatalf("quota = %d, want 100", quota)
	}
	if result == nil || result.MatchedTier != "fast" {
		t.Fatalf("matched tier = %v, want fast", result)
	}
}

func TestTryTieredSettleFallsBackToFrozenPreConsumeOnExprError(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		FinalPreConsumedQuota: 321,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:              "tiered_expr",
			ExprString:               `invalid +-+ expr`,
			ExprHash:                 billingexpr.ExprHashString(`invalid +-+ expr`),
			GroupRatio:               1.0,
			EstimatedQuotaAfterGroup: 123,
		},
	}

	ok, quota, result := TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 100})
	if !ok {
		t.Fatal("expected tiered settle to apply")
	}
	if quota != 321 {
		t.Fatalf("quota = %d, want 321", quota)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
}

// ---------------------------------------------------------------------------
// Pre-consume vs Post-consume consistency
// ---------------------------------------------------------------------------

func TestTryTieredSettle_PreConsumeMatchesPostConsume(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 1000, 500)
	params := billingexpr.TokenParams{P: 1000, C: 500}

	ok, quota, _ := TryTieredSettle(info, params)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 7000; quota = 7000 / 1M * 500K = 3500
	if quota != 3500 {
		t.Fatalf("quota = %d, want 3500", quota)
	}
	if quota != info.FinalPreConsumedQuota {
		t.Fatalf("pre-consume %d != post-consume %d", info.FinalPreConsumedQuota, quota)
	}
}

func TestTryTieredSettle_PostConsumeOverPreConsume(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 1000, 500)
	preConsumed := info.FinalPreConsumedQuota // 3500

	// Actual usage is higher than estimated
	params := billingexpr.TokenParams{P: 2000, C: 1000}
	ok, quota, _ := TryTieredSettle(info, params)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 14000; quota = 14000 / 1M * 500K = 7000
	if quota != 7000 {
		t.Fatalf("quota = %d, want 7000", quota)
	}
	if quota <= preConsumed {
		t.Fatalf("expected supplement: actual %d should > pre-consumed %d", quota, preConsumed)
	}
}

func TestTryTieredSettle_PostConsumeUnderPreConsume(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 1000, 500)
	preConsumed := info.FinalPreConsumedQuota // 3500

	// Actual usage is lower than estimated
	params := billingexpr.TokenParams{P: 100, C: 50}
	ok, quota, _ := TryTieredSettle(info, params)
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 700; quota = 700 / 1M * 500K = 350
	if quota != 350 {
		t.Fatalf("quota = %d, want 350", quota)
	}
	if quota >= preConsumed {
		t.Fatalf("expected refund: actual %d should < pre-consumed %d", quota, preConsumed)
	}
}

// ---------------------------------------------------------------------------
// Tiered boundary conditions
// ---------------------------------------------------------------------------

func TestTryTieredSettle_ExactBoundary(t *testing.T) {
	info := makeRelayInfo(sonnetTieredExpr, 1.0, 200000, 1000)

	// p == 200000 => standard tier (p <= 200000)
	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 200000, C: 1000})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// standard: p*1.5 + c*7.5 = 307500; quota = 307500 / 1M * 500K = 153750
	if quota != 153750 {
		t.Fatalf("quota = %d, want 153750", quota)
	}
	if result.MatchedTier != "standard" {
		t.Fatalf("tier = %s, want standard", result.MatchedTier)
	}
}

func TestTryTieredSettle_BoundaryPlusOne(t *testing.T) {
	info := makeRelayInfo(sonnetTieredExpr, 1.0, 200000, 1000)

	// p == 200001 => crosses to long_context tier
	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 200001, C: 1000})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// long_context: p*3 + c*11.25 = 611253; quota = round(611253 / 1M * 500K) = 305627
	if quota != 305627 {
		t.Fatalf("quota = %d, want 305627", quota)
	}
	if result.MatchedTier != "long_context" {
		t.Fatalf("tier = %s, want long_context", result.MatchedTier)
	}
	if !result.CrossedTier {
		t.Fatal("expected CrossedTier = true")
	}
}

func TestTryTieredSettle_ZeroTokens(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 0, 0)

	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 0, C: 0})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	if quota != 0 {
		t.Fatalf("quota = %d, want 0", quota)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
}

func TestTryTieredSettle_HugeTokens(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.0, 10000000, 5000000)

	ok, quota, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 10000000, C: 5000000})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 70000000; quota = 70000000 / 1M * 500K = 35000000
	if quota != 35000000 {
		t.Fatalf("quota = %d, want 35000000", quota)
	}
}

func TestTryTieredSettle_CacheTokensAffectSettlement(t *testing.T) {
	info := makeRelayInfo(cacheExpr, 1.0, 1000, 500)

	// Without cache tokens
	ok1, quota1, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok1 {
		t.Fatal("expected tiered settle")
	}
	// p*2 + c*10 = 7000; quota = 7000 / 1M * 500K = 3500

	// With cache tokens
	ok2, quota2, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500, CR: 10000, CC: 5000, CC1h: 2000})
	if !ok2 {
		t.Fatal("expected tiered settle")
	}
	// 2000 + 5000 + 2000 + 12500 + 8000 = 29500; quota = 29500 / 1M * 500K = 14750

	if quota2 <= quota1 {
		t.Fatalf("cache tokens should increase quota: without=%d, with=%d", quota1, quota2)
	}
	if quota1 != 3500 {
		t.Fatalf("no-cache quota = %d, want 3500", quota1)
	}
	if quota2 != 14750 {
		t.Fatalf("cache quota = %d, want 14750", quota2)
	}
}

// ---------------------------------------------------------------------------
// Request probe tests
// ---------------------------------------------------------------------------

func TestTryTieredSettle_RequestProbeInfluencesBilling(t *testing.T) {
	info := makeRelayInfo(probeExpr, 1.0, 1000, 500)
	info.BillingRequestInput = &billingexpr.RequestInput{
		Body: []byte(`{"service_tier":"fast"}`),
	}

	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// fast: p*4 + c*20 = 14000; quota = 14000 / 1M * 500K = 7000
	if quota != 7000 {
		t.Fatalf("quota = %d, want 7000", quota)
	}
	if result.MatchedTier != "fast" {
		t.Fatalf("tier = %s, want fast", result.MatchedTier)
	}
}

func TestTryTieredSettle_NoRequestInput_FallsBackToDefault(t *testing.T) {
	info := makeRelayInfo(probeExpr, 1.0, 1000, 500)
	// No BillingRequestInput set — param("service_tier") returns nil, not "fast"

	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// normal: p*2 + c*10 = 7000; quota = 7000 / 1M * 500K = 3500
	if quota != 3500 {
		t.Fatalf("quota = %d, want 3500", quota)
	}
	if result.MatchedTier != "normal" {
		t.Fatalf("tier = %s, want normal", result.MatchedTier)
	}
}

// ---------------------------------------------------------------------------
// Group ratio tests
// ---------------------------------------------------------------------------

func TestTryTieredSettle_GroupRatioScaling(t *testing.T) {
	info := makeRelayInfo(flatExpr, 1.5, 1000, 500)

	ok, quota, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	// exprCost = 7000, quotaBeforeGroup = 3500, afterGroup = round(3500 * 1.5) = 5250
	if quota != 5250 {
		t.Fatalf("quota = %d, want 5250", quota)
	}
}

func TestTryTieredSettle_GroupRatioZero(t *testing.T) {
	info := makeRelayInfo(flatExpr, 0, 1000, 500)

	ok, quota, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if !ok {
		t.Fatal("expected tiered settle")
	}
	if quota != 0 {
		t.Fatalf("quota = %d, want 0 (group ratio = 0)", quota)
	}
}

// ---------------------------------------------------------------------------
// Ratio mode (negative tests) — TryTieredSettle must return false
// ---------------------------------------------------------------------------

func TestTryTieredSettle_RatioMode_NilSnapshot(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: nil,
	}

	ok, _, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if ok {
		t.Fatal("expected TryTieredSettle to return false when snapshot is nil")
	}
}

func TestTryTieredSettle_RatioMode_WrongBillingMode(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "ratio",
			ExprString:  flatExpr,
			ExprHash:    billingexpr.ExprHashString(flatExpr),
			GroupRatio:  1.0,
		},
	}

	ok, _, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if ok {
		t.Fatal("expected TryTieredSettle to return false for ratio billing mode")
	}
}

func TestTryTieredSettle_RatioMode_EmptyBillingMode(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "",
			ExprString:  flatExpr,
			ExprHash:    billingexpr.ExprHashString(flatExpr),
			GroupRatio:  1.0,
		},
	}

	ok, _, _ := TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 500})
	if ok {
		t.Fatal("expected TryTieredSettle to return false for empty billing mode")
	}
}

// ---------------------------------------------------------------------------
// Fallback tests
// ---------------------------------------------------------------------------

func TestTryTieredSettle_ErrorFallbackToEstimatedQuotaAfterGroup(t *testing.T) {
	info := &relaycommon.RelayInfo{
		FinalPreConsumedQuota: 0,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:              "tiered_expr",
			ExprString:               `invalid expr!!!`,
			ExprHash:                 billingexpr.ExprHashString(`invalid expr!!!`),
			GroupRatio:               1.0,
			EstimatedQuotaAfterGroup: 999,
		},
	}

	ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{P: 100})
	if !ok {
		t.Fatal("expected tiered settle to apply")
	}
	// FinalPreConsumedQuota is 0, should fall back to EstimatedQuotaAfterGroup
	if quota != 999 {
		t.Fatalf("quota = %d, want 999", quota)
	}
	if result != nil {
		t.Fatal("result should be nil on error fallback")
	}
}
