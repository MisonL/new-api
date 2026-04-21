package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetConfiguredModelRatioFireworksModels(t *testing.T) {
	InitRatioSettings()

	tests := []struct {
		name      string
		wantRatio float64
		wantComp  float64
		wantCache *float64
	}{
		{name: "qwen3-vl-30b-a3b-thinking", wantRatio: 0.075, wantComp: 4, wantCache: floatPtr(0.07 / 0.15)},
		{name: "qwen3-vl-30b-a3b-instruct", wantRatio: 0.075, wantComp: 4, wantCache: nil},
		{name: "qwen3-8b", wantRatio: 0.1, wantComp: 1, wantCache: floatPtr(0.5)},
		{name: "minimax-m2p5", wantRatio: 0.15, wantComp: 4, wantCache: floatPtr(0.1)},
		{name: "llama-v3p3-70b-instruct", wantRatio: 0.45, wantComp: 1, wantCache: floatPtr(0.5)},
		{name: "kimi-k2p5", wantRatio: 0.3, wantComp: 5, wantCache: floatPtr(1.0 / 6.0)},
		{name: "gpt-oss-20b", wantRatio: 0.035, wantComp: 30.0 / 7.0, wantCache: floatPtr(4.0 / 7.0)},
		{name: "gpt-oss-120b", wantRatio: 0.075, wantComp: 4, wantCache: floatPtr(1.0 / 15.0)},
		{name: "glm-5", wantRatio: 0.5, wantComp: 3.2, wantCache: floatPtr(0.2)},
		{name: "glm-4p7", wantRatio: 0.3, wantComp: 11.0 / 3.0, wantCache: floatPtr(0.5)},
		{name: "deepseek-v3p2", wantRatio: 0.28, wantComp: 3, wantCache: floatPtr(0.5)},
		{name: "deepseek-v3p1", wantRatio: 0.28, wantComp: 3, wantCache: floatPtr(0.5)},
		{name: "kimi-k2-thinking", wantRatio: 0.3, wantComp: 25.0 / 6.0, wantCache: floatPtr(0.25)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio, ok, matchName := GetConfiguredModelRatio(tt.name)
			require.True(t, ok)
			require.Equal(t, tt.name, matchName)
			require.InDelta(t, tt.wantRatio, ratio, 1e-9)
			require.InDelta(t, tt.wantComp, GetCompletionRatio(tt.name), 1e-9)

			cacheRatio, hasCache := GetCacheRatio(tt.name)
			if tt.wantCache == nil {
				require.False(t, hasCache)
				return
			}
			require.True(t, hasCache)
			require.InDelta(t, *tt.wantCache, cacheRatio, 1e-9)
		})
	}
}

func TestGetConfiguredModelRatioMissingModel(t *testing.T) {
	InitRatioSettings()

	ratio, ok, matchName := GetConfiguredModelRatio("missing-model-for-pricing-test")
	require.False(t, ok)
	require.Zero(t, ratio)
	require.Equal(t, "missing-model-for-pricing-test", matchName)
}

func floatPtr(v float64) *float64 {
	return &v
}
