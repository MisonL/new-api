package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// New-model default pricing was added without any test that fails when a value
// is edited: the mutations below all leave the package green before this file
// existed. These cases pin the vendor list prices added for the models below,
// deriving each expected ratio from the published USD or RMB figure with the
// same conversion the maps use, so a drift in either the constant or the entry
// trips them.
//
// Prices are from each vendor's own price card; the same URLs are cited next to
// the entries in model_ratio.go. Cache values are asserted only where the
// vendor publishes a cached-input rate; a missing entry is asserted as missing
// so an invented discount cannot pass as intentional.
func TestRecentVendorModelPricingIsPinned(t *testing.T) {
	resetPricingMapsForTest(t)
	InitRatioSettings()

	type pricing struct {
		model            string
		modelRatio       float64
		completionRatio  float64
		cacheRatio       *float64
		createCacheRatio *float64
	}

	cases := []pricing{
		// claude-fable-5-1: $10 / 1M input, $50 / 1M output, $1.25 cache write,
		// $2.50 cache read.
		{model: "claude-fable-5-1", modelRatio: 10.0 / 2, completionRatio: 5, cacheRatio: floatPtr(0.025), createCacheRatio: floatPtr(1.25)},
		// claude-opus-5-5: $4 / 1M input, $20 / 1M output.
		{model: "claude-opus-5-5", modelRatio: 4.0 / 2, completionRatio: 5, cacheRatio: floatPtr(0.05), createCacheRatio: floatPtr(1.25)},
		// gemini-3.8-flash: $1.50 / 1M input standard price (the 2026 promotion
		// is deliberately not used).
		{model: "gemini-3.8-flash", modelRatio: 1.50 / 2, completionRatio: 5, cacheRatio: floatPtr(0.1)},
		// step-5-preview: ¥7 / 1M input, ¥20 / 1M output, ¥0.35 / 1M cache read.
		{model: "step-5-preview", modelRatio: 7.0 / 1000 * RMB, completionRatio: 20.0 / 7.0, cacheRatio: floatPtr(0.35 / 7.0)},
		// mimo-v2.6-flash: $0.14 / 1M input, $0.28 / 1M output, $0.0028 cache read.
		{model: "mimo-v2.6-flash", modelRatio: 0.14 / 2, completionRatio: 0.28 / 0.14, cacheRatio: floatPtr(0.0028 / 0.14)},
		// mimo-v2.6-pro: $0.435 / 1M input, $0.87 / 1M output, $0.0036 cache read.
		{model: "mimo-v2.6-pro", modelRatio: 0.435 / 2, completionRatio: 0.87 / 0.435, cacheRatio: floatPtr(0.0036 / 0.435)},
		// mimo-v2.6-pro-ultraspeed: $4.35 / 1M input, $8.70 / 1M output, $0.036 cache read.
		{model: "mimo-v2.6-pro-ultraspeed", modelRatio: 4.35 / 2, completionRatio: 8.7 / 4.35, cacheRatio: floatPtr(0.036 / 4.35)},
		// qwen3-embedding: $0.04 / $0.02 / $0.01 per 1M input; embeddings are
		// input-only, so completion is 1 and there is no cache discount.
		{model: "qwen3-embedding-8b", modelRatio: 0.04 / 2, completionRatio: 1},
		{model: "qwen3-embedding-4b", modelRatio: 0.02 / 2, completionRatio: 1},
		{model: "qwen3-embedding-0.6b", modelRatio: 0.01 / 2, completionRatio: 1},
		// gpt-6-sol: $2 / 1M input, $10 / 1M output, $0.20 cache read, $2.50
		// cache write. Output includes the published reasoning-token rate.
		{model: "gpt-6-sol", modelRatio: 2.0 / 2, completionRatio: 5, cacheRatio: floatPtr(0.1), createCacheRatio: floatPtr(1.25)},
		// gpt-6-luna: $0.10 / 1M input, $0.50 / 1M output.
		{model: "gpt-6-luna", modelRatio: 0.1 / 2, completionRatio: 5, cacheRatio: floatPtr(0.1), createCacheRatio: floatPtr(1.25)},
		// grok-4.7: $2 / 1M input, $6 / 1M output, $0.50 cache read.
		{model: "grok-4.7", modelRatio: 2.0 / 2, completionRatio: 3, cacheRatio: floatPtr(0.25)},
		// glm-5.3-flashx: $0.37 / 1M input, $1.25 / 1M output, $0.075 cache read.
		{model: "glm-5.3-flashx", modelRatio: 0.37 / 2, completionRatio: 1.25 / 0.37, cacheRatio: floatPtr(0.075 / 0.37)},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(tc.model)
			require.True(t, ok, "%s must be configured, not fall back to the 37.5 default", tc.model)
			require.Equal(t, tc.model, matchName)
			require.InDelta(t, tc.modelRatio, ratio, 1e-12)
			require.InDelta(t, tc.completionRatio, GetCompletionRatio(tc.model), 1e-12)

			cacheRatio, hasCacheRatio := GetCacheRatio(tc.model)
			require.Equal(t, tc.cacheRatio != nil, hasCacheRatio,
				"%s cache-read price presence must match the vendor card", tc.model)
			if tc.cacheRatio != nil {
				require.InDelta(t, *tc.cacheRatio, cacheRatio, 1e-12)
			}

			createCacheRatio, hasCreateCacheRatio := GetCreateCacheRatio(tc.model)
			require.Equal(t, tc.createCacheRatio != nil, hasCreateCacheRatio,
				"%s cache-write price presence must match the vendor card", tc.model)
			if tc.createCacheRatio != nil {
				require.InDelta(t, *tc.createCacheRatio, createCacheRatio, 1e-12)
			}
		})
	}
}
