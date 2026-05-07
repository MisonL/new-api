package model

import (
	"strings"
	"testing"

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
		ModelName:      "gpt-test",
		Group:          "default",
		BucketTs:       100,
		RequestCount:   1,
		SuccessCount:   1,
		TotalLatencyMs: 10,
		TtftSumMs:      2,
		TtftCount:      1,
		OutputTokens:   3,
		GenerationMs:   4,
	})

	require.Equal(t, []clause.Column{
		{Name: "model_name"},
		{Name: `"group"`, Raw: true},
		{Name: "bucket_ts"},
	}, upsertClause.Columns)

	expected := map[string]string{
		"request_count":    "perf_metrics.request_count + ?",
		"success_count":    "perf_metrics.success_count + ?",
		"total_latency_ms": "perf_metrics.total_latency_ms + ?",
		"ttft_sum_ms":      "perf_metrics.ttft_sum_ms + ?",
		"ttft_count":       "perf_metrics.ttft_count + ?",
		"output_tokens":    "perf_metrics.output_tokens + ?",
		"generation_ms":    "perf_metrics.generation_ms + ?",
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
