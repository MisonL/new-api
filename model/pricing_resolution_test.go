package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestResolvePricingUsesConfiguredRatioOnly(t *testing.T) {
	ratio_setting.InitRatioSettings()

	pricing := resolvePricing("missing-model-for-pricing-test")
	require.Zero(t, pricing.QuotaType)
	require.Zero(t, pricing.ModelRatio)
	require.Zero(t, pricing.ModelPrice)
	require.Zero(t, pricing.CompletionRatio)
}

func TestResolvePricingUsesFireworksDefaults(t *testing.T) {
	ratio_setting.InitRatioSettings()

	pricing := resolvePricing("deepseek-v3p2")
	require.Equal(t, 0, pricing.QuotaType)
	require.InDelta(t, 0.28, pricing.ModelRatio, 1e-9)
	require.InDelta(t, 3.0, pricing.CompletionRatio, 1e-9)
	require.Zero(t, pricing.ModelPrice)
}
