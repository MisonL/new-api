package model

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/clause"
)

func TestUpsertPerfMetricQualifiesIncrementColumns(t *testing.T) {
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousUsingMySQL := common.UsingMySQL
	previousUsingSQLite := common.UsingSQLite

	common.UsingPostgreSQL = true
	common.UsingMySQL = false
	common.UsingSQLite = false
	initCol()

	t.Cleanup(func() {
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.UsingMySQL = previousUsingMySQL
		common.UsingSQLite = previousUsingSQLite
		initCol()
	})

	upsertClause := perfMetricUpsertClause(&PerfMetric{
		ModelName:           "gpt-test",
		Group:               "default",
		BucketTs:            100,
		RequestCount:        1,
		SuccessCount:        1,
		TotalLatencyMs:      10,
		TtftSumMs:           2,
		TtftCount:           1,
		OutputTokens:        3,
		GenerationMs:        4,
		UpstreamHeaderMs:    5,
		UpstreamHeaderCount: 1,
		UpstreamTtfbMs:      6,
		UpstreamTtfbCount:   1,
		UpstreamTotalMs:     7,
		UpstreamTotalCount:  1,
	})

	require.Equal(t, []clause.Column{
		{Name: "model_name"},
		{Name: `"group"`, Raw: true},
		{Name: "bucket_ts"},
	}, upsertClause.Columns)

	expected := map[string]string{
		"request_count":                              "perf_metrics.request_count + ?",
		"success_count":                              "perf_metrics.success_count + ?",
		"total_latency_ms":                           "perf_metrics.total_latency_ms + ?",
		"ttft_sum_ms":                                "perf_metrics.ttft_sum_ms + ?",
		"ttft_count":                                 "perf_metrics.ttft_count + ?",
		"output_tokens":                              "perf_metrics.output_tokens + ?",
		"generation_ms":                              "perf_metrics.generation_ms + ?",
		"upstream_header_ms":                         "perf_metrics.upstream_header_ms + ?",
		"upstream_header_count":                      "perf_metrics.upstream_header_count + ?",
		"upstream_ttfb_ms":                           "perf_metrics.upstream_ttfb_ms + ?",
		"upstream_ttfb_count":                        "perf_metrics.upstream_ttfb_count + ?",
		"upstream_total_ms":                          "perf_metrics.upstream_total_ms + ?",
		"upstream_total_count":                       "perf_metrics.upstream_total_count + ?",
		"responses_bootstrap_recovery_count":         "perf_metrics.responses_bootstrap_recovery_count + ?",
		"responses_bootstrap_recovery_success_count": "perf_metrics.responses_bootstrap_recovery_success_count + ?",
		"responses_bootstrap_recovery_wait_ms":       "perf_metrics.responses_bootstrap_recovery_wait_ms + ?",
	}
	require.Len(t, upsertClause.DoUpdates, len(expected))
	for _, assignment := range upsertClause.DoUpdates {
		expr, ok := assignment.Value.(clause.Expr)
		require.True(t, ok, "assignment %s should use clause.Expr", assignment.Column.Name)
		require.Equal(t, expected[assignment.Column.Name], expr.SQL)
		require.True(t, strings.HasPrefix(expr.SQL, "perf_metrics."))
		require.NotEqual(t, assignment.Column.Name+" + ?", expr.SQL)
	}
}

func TestGetPerfMetricsSummaryAllFiltersGroups(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	require.NoError(t, DB.Create(&[]PerfMetric{
		{
			ModelName:           "gpt-test",
			Group:               "default",
			BucketTs:            now,
			RequestCount:        2,
			SuccessCount:        2,
			TotalLatencyMs:      200,
			OutputTokens:        40,
			GenerationMs:        20,
			UpstreamHeaderMs:    40,
			UpstreamHeaderCount: 2,
			UpstreamTtfbMs:      80,
			UpstreamTtfbCount:   2,
			UpstreamTotalMs:     160,
			UpstreamTotalCount:  2,
		},
		{
			ModelName:           "gpt-test",
			Group:               "legacy",
			BucketTs:            now,
			RequestCount:        9,
			SuccessCount:        9,
			TotalLatencyMs:      900,
			OutputTokens:        90,
			GenerationMs:        30,
			UpstreamHeaderMs:    90,
			UpstreamHeaderCount: 9,
			UpstreamTtfbMs:      270,
			UpstreamTtfbCount:   9,
			UpstreamTotalMs:     540,
			UpstreamTotalCount:  9,
		},
	}).Error)

	summaries, err := GetPerfMetricsSummaryAll(now-1, now+1, []string{"default"})

	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, "gpt-test", summaries[0].ModelName)
	require.Equal(t, int64(2), summaries[0].RequestCount)
	require.Equal(t, int64(2), summaries[0].SuccessCount)
	require.Equal(t, int64(200), summaries[0].TotalLatencyMs)
	require.Equal(t, int64(40), summaries[0].OutputTokens)
	require.Equal(t, int64(20), summaries[0].GenerationMs)
	require.Equal(t, int64(40), summaries[0].UpstreamHeaderMs)
	require.Equal(t, int64(2), summaries[0].UpstreamHeaderCount)
	require.Equal(t, int64(80), summaries[0].UpstreamTtfbMs)
	require.Equal(t, int64(2), summaries[0].UpstreamTtfbCount)
	require.Equal(t, int64(160), summaries[0].UpstreamTotalMs)
	require.Equal(t, int64(2), summaries[0].UpstreamTotalCount)
}

func TestUpsertAndSummaryPerfMetricAllowsBootstrapOnly(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	require.NoError(t, UpsertPerfMetric(&PerfMetric{
		ModelName:                              "gpt-test",
		Group:                                  "default",
		BucketTs:                               now,
		ResponsesBootstrapRecoveryCount:        2,
		ResponsesBootstrapRecoverySuccessCount: 1,
		ResponsesBootstrapRecoveryWaitMs:       3000,
	}))

	var stored PerfMetric
	require.NoError(t, DB.Where("model_name = ? AND "+commonGroupCol+" = ? AND bucket_ts = ?", "gpt-test", "default", now).First(&stored).Error)
	require.Equal(t, int64(0), stored.RequestCount)
	require.Equal(t, int64(2), stored.ResponsesBootstrapRecoveryCount)
	require.Equal(t, int64(1), stored.ResponsesBootstrapRecoverySuccessCount)
	require.Equal(t, int64(3000), stored.ResponsesBootstrapRecoveryWaitMs)

	summaries, err := GetPerfMetricsSummaryAll(now-1, now+1, []string{"default"})

	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, "gpt-test", summaries[0].ModelName)
	require.Equal(t, int64(0), summaries[0].RequestCount)
	require.Equal(t, int64(2), summaries[0].ResponsesBootstrapRecoveryCount)
	require.Equal(t, int64(1), summaries[0].ResponsesBootstrapRecoverySuccessCount)
	require.Equal(t, int64(3000), summaries[0].ResponsesBootstrapRecoveryWaitMs)
}
