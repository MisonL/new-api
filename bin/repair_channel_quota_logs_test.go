package main

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestDSNCanBeReadFromExplicitEnvironmentVariable(t *testing.T) {
	const envName = "NEW_API_PRICING_REPAIR_TEST_DSN"
	t.Setenv(envName, "postgres://example.invalid/db")
	require.Equal(t, "postgres://example.invalid/db", os.Getenv(envName))
}

func TestConfigureCryptoSecretFromEnvRequiresStableSecret(t *testing.T) {
	original := common.CryptoSecret
	t.Cleanup(func() { common.CryptoSecret = original })

	t.Setenv("CRYPTO_SECRET", "repair-crypto-secret")
	require.NoError(t, configureCryptoSecretFromEnv())
	require.Equal(t, "repair-crypto-secret", common.CryptoSecret)

	t.Setenv("CRYPTO_SECRET", "")
	require.ErrorContains(t, configureCryptoSecretFromEnv(), "requires CRYPTO_SECRET")
	t.Setenv("CRYPTO_SECRET", "random_string")
	require.ErrorContains(t, configureCryptoSecretFromEnv(), "stable random string")
}

func TestValidateApplyCachePrerequisitesRequiresExplicitCachePolicy(t *testing.T) {
	t.Setenv("CRYPTO_SECRET", "repair-crypto-secret")
	require.ErrorContains(t, validateApplyCachePrerequisites("", false), "requires Redis DSN")
	require.NoError(t, validateApplyCachePrerequisites("", true))
	require.ErrorContains(t, validateApplyCachePrerequisites("not-a-redis-url", false), "parse Redis DSN")
}

func TestApplyPlanRejectsMissingRedisBeforeOpeningTransaction(t *testing.T) {
	err := applyPlan(nil, &plan{}, "", false)
	require.ErrorContains(t, err, "requires Redis DSN")
}

func TestParseModelFilterNormalizesAndDeduplicates(t *testing.T) {
	require.Equal(t, []string{"gemini-2.5-flash", "gemini-3-flash-preview"}, parseModelFilter(" Gemini-2.5-Flash,gemini-3-flash-preview,gemini-2.5-flash,, "))
}

func TestParseModelFilterEmptyForAllMode(t *testing.T) {
	require.Empty(t, parseModelFilter(""))
}

func TestParseModelFilterSupportsExcludedModels(t *testing.T) {
	require.Equal(t, []string{"grok-4.5", "gpt-oss-20b"}, parseModelFilter("Grok-4.5,gpt-oss-20b,grok-4.5"))
}

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
			"model_price":      -1,
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

func TestRepairRowIgnoresJSONKeyOrderingWhenValuesAreUnchanged(t *testing.T) {
	ratio_setting.InitRatioSettings()

	row := logRow{
		ID:           9,
		UserID:       1,
		Username:     "tester",
		ChannelID:    106,
		ModelName:    "gpt-5.6-luna",
		Quota:        100,
		PromptTokens: 1000,
		Other:        `{"completion_ratio":6,"model_ratio":0.1,"model_price":-1,"billing_source":"wallet","group_ratio":1,"cache_ratio":0.1}`,
	}

	_, ok, err := repairRow(row)
	require.NoError(t, err)
	require.False(t, ok)
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
			"model_price":      -1,
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
			"model_price":      -1,
			"model_ratio":      37.5,
			"completion_ratio": 1,
			"cache_ratio":      1,
		}),
	}

	_, ok, err := repairRow(row)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRepairRowUsesAnthropicCacheCreationSemantics(t *testing.T) {
	ratio_setting.InitRatioSettings()

	row := logRow{
		ID:               4,
		UserID:           1,
		Username:         "tester",
		TokenID:          0,
		ChannelID:        106,
		ModelName:        "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 100,
		Other: common.MapToJsonStr(map[string]interface{}{
			"billing_source":           "wallet",
			"group_ratio":              1,
			"cache_tokens":             400,
			"cache_creation_tokens":    200,
			"cache_creation_tokens_5m": 100,
			"cache_creation_tokens_1h": 50,
			"usage_semantic":           "anthropic",
			"model_price":              -1,
			"model_ratio":              99,
			"completion_ratio":         1,
		}),
	}

	repaired, ok, err := repairRow(row)
	require.NoError(t, err)
	require.True(t, ok)
	// (1000 + 400*0.1 + 50*1.25 + 100*1.25 + 50*2 + 100*5) * 1.5
	require.Equal(t, int64(2741), repaired.NewQuota)

	updated, err := common.StrToMap(repaired.NewOther)
	require.NoError(t, err)
	require.InDelta(t, 1.25, numberValue(updated["cache_creation_ratio"]), 1e-9)
	require.InDelta(t, 1.25, numberValue(updated["cache_creation_ratio_5m"]), 1e-9)
	require.InDelta(t, 2.0, numberValue(updated["cache_creation_ratio_1h"]), 1e-9)
}

func TestRepairRowHonorsExplicitZeroGroupRatio(t *testing.T) {
	ratio_setting.InitRatioSettings()

	row := logRow{
		ID:               5,
		UserID:           1,
		Username:         "tester",
		ChannelID:        106,
		ModelName:        "glm-5",
		PromptTokens:     100,
		CompletionTokens: 100,
		Other: common.MapToJsonStr(map[string]interface{}{
			"billing_source":   "wallet",
			"group_ratio":      0,
			"model_price":      -1,
			"model_ratio":      37.5,
			"completion_ratio": 1,
		}),
	}

	repaired, ok, err := repairRow(row)
	require.NoError(t, err)
	require.True(t, ok)
	require.Zero(t, repaired.NewQuota)
}

func TestRepairRowRejectsIncompleteOrInvalidPriceSnapshots(t *testing.T) {
	ratio_setting.InitRatioSettings()

	base := map[string]interface{}{
		"billing_source":   "wallet",
		"group_ratio":      1,
		"model_price":      -1,
		"model_ratio":      1,
		"completion_ratio": 1,
	}
	cases := []struct {
		name   string
		mutate func(map[string]interface{})
	}{
		{name: "missing model price", mutate: func(values map[string]interface{}) { delete(values, "model_price") }},
		{name: "zero model price", mutate: func(values map[string]interface{}) { values["model_price"] = 0 }},
		{name: "positive model price", mutate: func(values map[string]interface{}) { values["model_price"] = 0.01 }},
		{name: "invalid model ratio", mutate: func(values map[string]interface{}) { values["model_ratio"] = "not-a-number" }},
		{name: "invalid completion ratio", mutate: func(values map[string]interface{}) { values["completion_ratio"] = "NaN" }},
		{name: "negative model ratio", mutate: func(values map[string]interface{}) { values["model_ratio"] = -1 }},
		{name: "tiered billing", mutate: func(values map[string]interface{}) { values["billing_mode"] = "tiered_expr" }},
		{name: "web search", mutate: func(values map[string]interface{}) { values["web_search"] = true }},
		{name: "file search", mutate: func(values map[string]interface{}) { values["file_search"] = true }},
		{name: "image generation", mutate: func(values map[string]interface{}) { values["image_generation_call"] = true }},
		{name: "audio", mutate: func(values map[string]interface{}) { values["audio_input_seperate_price"] = true }},
		{name: "image", mutate: func(values map[string]interface{}) { values["image"] = true }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]interface{}, len(base))
			for key, value := range base {
				values[key] = value
			}
			tc.mutate(values)
			row := logRow{
				ID:        6,
				UserID:    1,
				Username:  "tester",
				ChannelID: 106,
				ModelName: "glm-5",
				Other:     common.MapToJsonStr(values),
			}

			_, ok, err := repairRow(row)
			require.NoError(t, err)
			require.False(t, ok)
		})
	}

	for _, modelName := range []string{
		"gemini-3.1-pro-preview",
		"gpt-image-2",
		"gpt-4o-audio-preview-2024-10-01",
		"text-embedding-3-large",
		"deepseek-v4-flash-search",
		"sensenova-6.7-flash-lite",
	} {
		t.Run("non-token model/"+modelName, func(t *testing.T) {
			row := logRow{
				ID:        8,
				UserID:    1,
				Username:  "tester",
				ChannelID: 106,
				ModelName: modelName,
				Other: common.MapToJsonStr(map[string]interface{}{
					"billing_source":   "wallet",
					"model_price":      -1,
					"model_ratio":      1,
					"completion_ratio": 1,
				}),
			}
			_, ok, err := repairRow(row)
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestRepairRowSkipsImageRequestMetadata(t *testing.T) {
	ratio_setting.InitRatioSettings()

	base := map[string]interface{}{
		"billing_source":   "wallet",
		"model_price":      -1,
		"model_ratio":      1,
		"completion_ratio": 1,
	}
	cases := []struct {
		name   string
		mutate func(map[string]interface{})
	}{
		{name: "request path", mutate: func(values map[string]interface{}) {
			values["request_path"] = "/v1/images/generations?model=gpt-image-2"
		}},
		{name: "upstream request path", mutate: func(values map[string]interface{}) { values["upstream_request_path"] = "/v1/images/edits" }},
		{name: "conversion string", mutate: func(values map[string]interface{}) { values["request_conversion"] = "openai_image" }},
		{name: "conversion slice", mutate: func(values map[string]interface{}) {
			values["request_conversion"] = []interface{}{"OpenAI Compatible", "openai_image"}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]interface{}, len(base)+1)
			for key, value := range base {
				values[key] = value
			}
			tc.mutate(values)
			row := logRow{
				ID:        7,
				UserID:    1,
				Username:  "tester",
				ChannelID: 106,
				ModelName: "gpt-image-2",
				Other:     common.MapToJsonStr(values),
			}

			_, ok, err := repairRow(row)
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestRepairRowRepairsGrok45PathAliasFallbackWithOfficialTieredPrice(t *testing.T) {
	ratio_setting.InitRatioSettings()

	row := logRow{
		ID:               2002467,
		UserID:           1,
		Username:         "tester",
		ChannelID:        188,
		ModelName:        "xai/grok-4.5",
		PromptTokens:     208,
		CompletionTokens: 112,
		Quota:            12000,
		Other: common.MapToJsonStr(map[string]interface{}{
			"billing_source":   "wallet",
			"group_ratio":      1,
			"model_price":      -1,
			"model_ratio":      37.5,
			"completion_ratio": 1,
			"cache_ratio":      1,
			"cache_tokens":     192,
			"request_conversion": []interface{}{
				"OpenAI Responses",
			},
		}),
	}

	repaired, ok, err := repairRow(row)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(381), repaired.NewQuota)

	updated, err := common.StrToMap(repaired.NewOther)
	require.NoError(t, err)
	require.Equal(t, billing_setting.BillingModeTieredExpr, updated["billing_mode"])
	require.Equal(t, "standard", updated["matched_tier"])
	require.Zero(t, numberValue(updated["model_ratio"]))
	require.Zero(t, numberValue(updated["completion_ratio"]))
	require.Zero(t, numberValue(updated["cache_ratio"]))
	exprBytes, err := base64.StdEncoding.DecodeString(stringValue(updated["expr_b64"]))
	require.NoError(t, err)
	require.Equal(t, billing_setting.Grok45BillingExpr, string(exprBytes))
}

func TestRepairRowKeepsHistoricalGrok45CacheSnapshot(t *testing.T) {
	ratio_setting.InitRatioSettings()

	row := logRow{
		ID:               1045641,
		UserID:           1,
		Username:         "tester",
		ChannelID:        168,
		ModelName:        "grok-4.5",
		PromptTokens:     211,
		CompletionTokens: 1,
		Quota:            118,
		Other: common.MapToJsonStr(map[string]interface{}{
			"billing_source":   "wallet",
			"group_ratio":      1,
			"model_price":      -1,
			"model_ratio":      1,
			"completion_ratio": 3,
			"cache_ratio":      0.25,
			"cache_tokens":     128,
		}),
	}

	_, ok, err := repairRow(row)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRepairRowNormalizesZeroCacheGrok45AliasMetadata(t *testing.T) {
	ratio_setting.InitRatioSettings()

	row := logRow{
		ID:               1689431,
		UserID:           1,
		Username:         "tester",
		ChannelID:        220,
		ModelName:        "grok-4-5",
		PromptTokens:     2202,
		CompletionTokens: 8,
		Quota:            2226,
		Other: common.MapToJsonStr(map[string]interface{}{
			"group_ratio":      1,
			"model_price":      -1,
			"model_ratio":      1,
			"completion_ratio": 3,
			"cache_ratio":      0.25,
			"cache_tokens":     0,
		}),
	}

	repaired, ok, err := repairRow(row)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(2226), repaired.NewQuota)
	updated, err := common.StrToMap(repaired.NewOther)
	require.NoError(t, err)
	require.Equal(t, billing_setting.BillingModeTieredExpr, updated["billing_mode"])
	require.Equal(t, "standard", updated["matched_tier"])
}
