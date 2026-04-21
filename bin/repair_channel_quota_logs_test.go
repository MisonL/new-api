package main

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestRepairRowRepairsMetadataWhenQuotaStaysZero(t *testing.T) {
	ratio_setting.InitRatioSettings()

	row := logRow{
		ID:        1,
		UserID:    2,
		Username:  "tester",
		TokenID:   3,
		ChannelID: 106,
		ModelName: "deepseek-v3p2",
		Quota:     0,
		Other: common.MapToJsonStr(map[string]interface{}{
			"billing_source":   "wallet",
			"group_ratio":      1,
			"cache_tokens":     0,
			"model_ratio":      37.5,
			"completion_ratio": 1,
			"cache_ratio":      1,
		}),
	}

	repaired, ok, err := repairRow(row)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(0), repaired.NewQuota)
	require.NotEqual(t, row.Other, repaired.NewOther)

	updated, err := common.StrToMap(repaired.NewOther)
	require.NoError(t, err)
	require.InDelta(t, 0.28, numberValue(updated["model_ratio"]), 1e-9)
	require.InDelta(t, 3.0, numberValue(updated["completion_ratio"]), 1e-9)
	require.InDelta(t, 0.5, numberValue(updated["cache_ratio"]), 1e-9)
}

func TestRepairRowTreatsMissingBillingSourceDirectUserAsWallet(t *testing.T) {
	ratio_setting.InitRatioSettings()

	row := logRow{
		ID:               2,
		UserID:           1,
		Username:         "tester",
		TokenID:          0,
		ChannelID:        106,
		ModelName:        "glm-5",
		Quota:            1875,
		PromptTokens:     0,
		CompletionTokens: 50,
		Other: common.MapToJsonStr(map[string]interface{}{
			"group_ratio":      1,
			"cache_tokens":     0,
			"model_ratio":      37.5,
			"completion_ratio": 1,
			"cache_ratio":      1,
		}),
	}

	repaired, ok, err := repairRow(row)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(80), repaired.NewQuota)
	require.Equal(t, int64(-1795), repaired.Delta)

	updated, err := common.StrToMap(repaired.NewOther)
	require.NoError(t, err)
	require.InDelta(t, 0.5, numberValue(updated["model_ratio"]), 1e-9)
	require.InDelta(t, 3.2, numberValue(updated["completion_ratio"]), 1e-9)
	require.InDelta(t, 0.2, numberValue(updated["cache_ratio"]), 1e-9)
}

func TestRepairRowSkipsMissingBillingSourceWhenTokenBacked(t *testing.T) {
	ratio_setting.InitRatioSettings()

	row := logRow{
		ID:        3,
		UserID:    1,
		Username:  "tester",
		TokenID:   9,
		ChannelID: 106,
		ModelName: "glm-5",
		Quota:     1875,
		Other: common.MapToJsonStr(map[string]interface{}{
			"group_ratio":      1,
			"cache_tokens":     0,
			"model_ratio":      37.5,
			"completion_ratio": 1,
			"cache_ratio":      1,
		}),
	}

	_, ok, err := repairRow(row)
	require.NoError(t, err)
	require.False(t, ok)
}
