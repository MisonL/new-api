package model

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestSumUsedQuotaUsesSingleFilteredConsumeStat(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)

	now := time.Now().Unix()
	logs := []*Log{
		{
			CreatedAt:         now - 10,
			Type:              LogTypeConsume,
			Username:          "alice",
			TokenName:         "codex-token",
			ModelName:         "gpt-5.5",
			Quota:             100,
			PromptTokens:      12,
			CompletionTokens:  8,
			ChannelId:         24,
			Group:             "default",
			RequestId:         "local-a",
			UpstreamRequestId: "upstream-a",
		},
		{
			CreatedAt:         now - 90,
			Type:              LogTypeConsume,
			Username:          "alice",
			TokenName:         "codex-token",
			ModelName:         "gpt-5.5",
			Quota:             60,
			PromptTokens:      30,
			CompletionTokens:  20,
			ChannelId:         24,
			Group:             "default",
			RequestId:         "local-b",
			UpstreamRequestId: "upstream-b",
		},
		{
			CreatedAt:        now - 5,
			Type:             LogTypeTopup,
			Username:         "alice",
			TokenName:        "codex-token",
			ModelName:        "gpt-5.5",
			Quota:            999,
			PromptTokens:     100,
			CompletionTokens: 100,
			ChannelId:        24,
			Group:            "default",
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	stat, err := SumUsedQuota(0, now-120, now+1, "gpt-5.5", "alice", "codex-token", 24, "default")

	require.NoError(t, err)
	require.Equal(t, 160, stat.Quota)
	require.Equal(t, 1, stat.Rpm)
	require.Equal(t, 20, stat.Tpm)

	filteredStat, err := SumUsedQuotaByFilter(LogFilter{
		StartTimestamp:    now - 120,
		EndTimestamp:      now + 1,
		RequestId:         "local-a",
		UpstreamRequestId: "upstream-a",
	})
	require.NoError(t, err)
	require.Equal(t, 100, filteredStat.Quota)
	require.Equal(t, 1, filteredStat.Rpm)
	require.Equal(t, 20, filteredStat.Tpm)
}

func TestSumUsedQuotaAggregatesCacheTokenStatsByUsageSemantic(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)

	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create([]Log{
		{
			CreatedAt:        now - 10,
			Type:             LogTypeConsume,
			Username:         "alice",
			ModelName:        "claude-3-7-sonnet",
			PromptTokens:     100,
			CompletionTokens: 20,
			Other:            `{"usage_semantic":"anthropic","cache_tokens":100,"cache_creation_tokens":40}`,
		},
		{
			CreatedAt:        now - 9,
			Type:             LogTypeConsume,
			Username:         "alice",
			ModelName:        "gpt-5.5",
			PromptTokens:     200,
			CompletionTokens: 30,
			Other:            `{"cache_tokens":50,"cache_creation_tokens_5m":10,"cache_creation_tokens_1h":20}`,
		},
		{
			CreatedAt:        now - 8,
			Type:             LogTypeTopup,
			Username:         "alice",
			PromptTokens:     999,
			CompletionTokens: 999,
			Other:            `{"cache_tokens":999}`,
		},
	}).Error)

	stat, err := SumUsedQuota(0, now-60, now+1, "", "alice", "", 0, "")
	require.NoError(t, err)
	require.Equal(t, 300, stat.InputTokens)
	require.Equal(t, 150, stat.CacheReadTokens)
	require.Equal(t, 70, stat.CacheWriteTokens)
	require.Equal(t, 50, stat.OutputTokens)
	// (100 / (100 + 100) + 50 / 200) weighted by each row denominator = 150 / 400.
	require.NotNil(t, stat.CacheRate)
	require.InDelta(t, 0.375, *stat.CacheRate, 1e-9)
}

func TestSumUsedQuotaLeavesCacheRateUnavailableWithoutInput(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)

	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        now - 10,
		Type:             LogTypeConsume,
		Username:         "alice",
		PromptTokens:     0,
		CompletionTokens: 10,
		Other:            `{"cache_tokens":0}`,
	}).Error)

	stat, err := SumUsedQuota(0, now-60, now+1, "", "alice", "", 0, "")
	require.NoError(t, err)
	require.Nil(t, stat.CacheRate)
}

func TestSumUsedQuotaCacheRateTelemetry(t *testing.T) {
	truncateTables(t)
	for _, tc := range []struct {
		name  string
		other string
		want  interface{}
	}{
		{"missing", `{}`, nil},
		{"null", `{"cache_tokens":null}`, nil},
		{"invalid", `{"cache_tokens":"invalid"}`, nil},
		{"negative", `{"cache_tokens":-1}`, nil},
		{"zero", `{"cache_tokens":0}`, float64(0)},
		{"string zero", `{"cache_tokens":"0"}`, float64(0)},
		{"openai", `{"cache_tokens":25}`, 0.25},
		{"anthropic", `{"cache_tokens":100,"usage_semantic":"anthropic"}`, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
			require.NoError(t, LOG_DB.Create(&Log{Type: LogTypeConsume, PromptTokens: 100, Other: tc.other}).Error)
			stat, err := SumUsedQuotaByFilter(LogFilter{})
			require.NoError(t, err)
			encoded, err := common.Marshal(stat)
			require.NoError(t, err)
			var result map[string]interface{}
			require.NoError(t, common.Unmarshal(encoded, &result))
			require.Equal(t, tc.want, result["cache_rate"])
		})
	}
}

func TestLogTokenCountSaturatesUnsafeNumbers(t *testing.T) {
	maxInt := uint64(^uint(0) >> 1)
	for _, tc := range []struct {
		name  string
		value interface{}
		want  uint64
	}{
		{name: "negative integer", value: int64(-1), want: 0},
		{name: "platform max integer", value: int64(maxInt), want: maxInt},
		{name: "huge float", value: 1e100, want: ^uint64(0)},
		{name: "positive infinity", value: math.Inf(1), want: 0},
		{name: "huge string", value: "1e100", want: ^uint64(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, logOtherTokenCount(tc.value))
		})
	}
	require.Equal(t, int(maxInt), logTokenCountToInt(^uint64(0)))
}

func TestSumUsedQuotaClampsOversizedCacheTelemetry(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, LOG_DB.Create(&Log{
		Type:         LogTypeConsume,
		PromptTokens: 1,
		Other:        `{"cache_tokens":1e100,"cache_creation_tokens_5m":1e100,"cache_creation_tokens_1h":1e100,"usage_semantic":"anthropic"}`,
	}).Error)

	stat, err := SumUsedQuotaByFilter(LogFilter{})
	require.NoError(t, err)
	maxInt := int(^uint(0) >> 1)
	require.Equal(t, maxInt, stat.CacheReadTokens)
	require.Equal(t, maxInt, stat.CacheWriteTokens)
	require.NotNil(t, stat.CacheRate)
	require.Equal(t, 1.0, *stat.CacheRate)
}

func TestSumUsedQuotaCacheRateIgnoresMissingTelemetryInMixedLogs(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, LOG_DB.Create([]Log{
		{Type: LogTypeConsume, PromptTokens: 100, Other: `{"cache_tokens":50}`},
		{Type: LogTypeConsume, PromptTokens: 900, Other: `{}`},
	}).Error)
	stat, err := SumUsedQuotaByFilter(LogFilter{})
	require.NoError(t, err)
	require.Equal(t, 1000, stat.InputTokens)
	require.NotNil(t, stat.CacheRate)
	require.Equal(t, 0.5, *stat.CacheRate)
}

func TestSumUsedQuotaStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := SumUsedQuotaByFilterContext(ctx, LogFilter{})
	require.ErrorIs(t, err, context.Canceled)
}

func BenchmarkSumLogTokenStats(b *testing.B) {
	require.NoError(b, LOG_DB.Exec("DELETE FROM logs").Error)
	defer LOG_DB.Exec("DELETE FROM logs")
	other, err := common.Marshal(map[string]interface{}{
		"cache_tokens":       50,
		"cache_write_tokens": 20,
		"diagnostics": []map[string]interface{}{
			{"headers": map[string]string{"content-type": "text/event-stream"}, "latency": 10},
			{"headers": map[string]string{"content-type": "text/event-stream"}, "latency": 20},
		},
	})
	require.NoError(b, err)
	logs := make([]Log, 1000)
	for i := range logs {
		logs[i] = Log{Type: LogTypeConsume, PromptTokens: 100, Other: string(other)}
	}
	require.NoError(b, LOG_DB.CreateInBatches(logs, 100).Error)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sumLogTokenStats(context.Background(), LogFilter{LogType: LogTypeConsume})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestSumUsedQuotaWildcardUsernameIsExplicit(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)

	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create([]Log{
		{
			CreatedAt:        now - 10,
			Type:             LogTypeConsume,
			Username:         "alice",
			TokenName:        "token-a",
			ModelName:        "gpt-5",
			Quota:            100,
			PromptTokens:     12,
			CompletionTokens: 8,
			Group:            "default",
		},
		{
			CreatedAt:        now - 10,
			Type:             LogTypeConsume,
			Username:         "alice-extra",
			TokenName:        "token-b",
			ModelName:        "gpt-5",
			Quota:            60,
			PromptTokens:     5,
			CompletionTokens: 5,
			Group:            "default",
		},
	}).Error)

	exactStat, err := SumUsedQuota(0, now-120, now+1, "gpt-5", "alice%", "", 0, "default")
	require.NoError(t, err)
	require.Equal(t, 0, exactStat.Quota)

	wildcardStat, err := SumUsedQuotaWithWildcardUsername(0, now-120, now+1, "gpt-5", "alice%", "", 0, "default")
	require.NoError(t, err)
	require.Equal(t, 160, wildcardStat.Quota)
	require.Equal(t, 2, wildcardStat.Rpm)
	require.Equal(t, 30, wildcardStat.Tpm)
}
