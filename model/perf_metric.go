package model

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PerfMetric stores aggregated relay performance metrics for the model square.
type PerfMetric struct {
	Id                                     int    `json:"id" gorm:"primaryKey"`
	ModelName                              string `json:"model_name" gorm:"size:128;uniqueIndex:idx_perf_model_group_bucket,priority:1"`
	Group                                  string `json:"group" gorm:"column:group;size:64;uniqueIndex:idx_perf_model_group_bucket,priority:2"`
	BucketTs                               int64  `json:"bucket_ts" gorm:"uniqueIndex:idx_perf_model_group_bucket,priority:3;index:idx_perf_bucket_ts"`
	RequestCount                           int64  `json:"request_count" gorm:"default:0"`
	SuccessCount                           int64  `json:"success_count" gorm:"default:0"`
	TotalLatencyMs                         int64  `json:"total_latency_ms" gorm:"default:0"`
	TtftSumMs                              int64  `json:"ttft_sum_ms" gorm:"default:0"`
	TtftCount                              int64  `json:"ttft_count" gorm:"default:0"`
	OutputTokens                           int64  `json:"output_tokens" gorm:"default:0"`
	GenerationMs                           int64  `json:"generation_ms" gorm:"default:0"`
	UpstreamHeaderMs                       int64  `json:"upstream_header_ms" gorm:"default:0"`
	UpstreamHeaderCount                    int64  `json:"upstream_header_count" gorm:"default:0"`
	UpstreamTtfbMs                         int64  `json:"upstream_ttfb_ms" gorm:"default:0"`
	UpstreamTtfbCount                      int64  `json:"upstream_ttfb_count" gorm:"default:0"`
	UpstreamTotalMs                        int64  `json:"upstream_total_ms" gorm:"default:0"`
	UpstreamTotalCount                     int64  `json:"upstream_total_count" gorm:"default:0"`
	ResponsesBootstrapRecoveryCount        int64  `json:"responses_bootstrap_recovery_count" gorm:"default:0"`
	ResponsesBootstrapRecoverySuccessCount int64  `json:"responses_bootstrap_recovery_success_count" gorm:"default:0"`
	ResponsesBootstrapRecoveryWaitMs       int64  `json:"responses_bootstrap_recovery_wait_ms" gorm:"default:0"`
}

func (PerfMetric) TableName() string {
	return "perf_metrics"
}

func perfMetricIncrementExpr(column string, value int64) clause.Expr {
	return gorm.Expr("perf_metrics."+column+" + ?", value)
}

func perfMetricUpsertClause(metric *PerfMetric) clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{
			{Name: "model_name"},
			{Name: commonGroupCol, Raw: true},
			{Name: "bucket_ts"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count":                              perfMetricIncrementExpr("request_count", metric.RequestCount),
			"success_count":                              perfMetricIncrementExpr("success_count", metric.SuccessCount),
			"total_latency_ms":                           perfMetricIncrementExpr("total_latency_ms", metric.TotalLatencyMs),
			"ttft_sum_ms":                                perfMetricIncrementExpr("ttft_sum_ms", metric.TtftSumMs),
			"ttft_count":                                 perfMetricIncrementExpr("ttft_count", metric.TtftCount),
			"output_tokens":                              perfMetricIncrementExpr("output_tokens", metric.OutputTokens),
			"generation_ms":                              perfMetricIncrementExpr("generation_ms", metric.GenerationMs),
			"upstream_header_ms":                         perfMetricIncrementExpr("upstream_header_ms", metric.UpstreamHeaderMs),
			"upstream_header_count":                      perfMetricIncrementExpr("upstream_header_count", metric.UpstreamHeaderCount),
			"upstream_ttfb_ms":                           perfMetricIncrementExpr("upstream_ttfb_ms", metric.UpstreamTtfbMs),
			"upstream_ttfb_count":                        perfMetricIncrementExpr("upstream_ttfb_count", metric.UpstreamTtfbCount),
			"upstream_total_ms":                          perfMetricIncrementExpr("upstream_total_ms", metric.UpstreamTotalMs),
			"upstream_total_count":                       perfMetricIncrementExpr("upstream_total_count", metric.UpstreamTotalCount),
			"responses_bootstrap_recovery_count":         perfMetricIncrementExpr("responses_bootstrap_recovery_count", metric.ResponsesBootstrapRecoveryCount),
			"responses_bootstrap_recovery_success_count": perfMetricIncrementExpr("responses_bootstrap_recovery_success_count", metric.ResponsesBootstrapRecoverySuccessCount),
			"responses_bootstrap_recovery_wait_ms":       perfMetricIncrementExpr("responses_bootstrap_recovery_wait_ms", metric.ResponsesBootstrapRecoveryWaitMs),
		}),
	}
}

func UpsertPerfMetric(metric *PerfMetric) error {
	if metric == nil || (metric.RequestCount == 0 && metric.ResponsesBootstrapRecoveryCount == 0) {
		return nil
	}
	return DB.Clauses(perfMetricUpsertClause(metric)).Create(metric).Error
}

func GetPerfMetrics(modelName string, group string, startTs int64, endTs int64) ([]PerfMetric, error) {
	var metrics []PerfMetric
	query := DB.Model(&PerfMetric{}).
		Where("model_name = ? AND bucket_ts >= ? AND bucket_ts <= ?", modelName, startTs, endTs)
	if group != "" {
		query = query.Where(commonGroupCol+" = ?", group)
	}
	err := query.Order("bucket_ts ASC").Find(&metrics).Error
	return metrics, err
}

type PerfMetricSummary struct {
	ModelName                              string `json:"model_name"`
	RequestCount                           int64  `json:"request_count"`
	SuccessCount                           int64  `json:"success_count"`
	TotalLatencyMs                         int64  `json:"total_latency_ms"`
	OutputTokens                           int64  `json:"output_tokens"`
	GenerationMs                           int64  `json:"generation_ms"`
	UpstreamHeaderMs                       int64  `json:"upstream_header_ms"`
	UpstreamHeaderCount                    int64  `json:"upstream_header_count"`
	UpstreamTtfbMs                         int64  `json:"upstream_ttfb_ms"`
	UpstreamTtfbCount                      int64  `json:"upstream_ttfb_count"`
	UpstreamTotalMs                        int64  `json:"upstream_total_ms"`
	UpstreamTotalCount                     int64  `json:"upstream_total_count"`
	ResponsesBootstrapRecoveryCount        int64  `json:"responses_bootstrap_recovery_count"`
	ResponsesBootstrapRecoverySuccessCount int64  `json:"responses_bootstrap_recovery_success_count"`
	ResponsesBootstrapRecoveryWaitMs       int64  `json:"responses_bootstrap_recovery_wait_ms"`
}

func GetPerfMetricsSummaryAll(startTs int64, endTs int64, groups []string) ([]PerfMetricSummary, error) {
	var summaries []PerfMetricSummary
	if groups != nil && len(groups) == 0 {
		return summaries, nil
	}
	query := DB.Model(&PerfMetric{}).
		Select("model_name, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms, SUM(upstream_header_ms) as upstream_header_ms, SUM(upstream_header_count) as upstream_header_count, SUM(upstream_ttfb_ms) as upstream_ttfb_ms, SUM(upstream_ttfb_count) as upstream_ttfb_count, SUM(upstream_total_ms) as upstream_total_ms, SUM(upstream_total_count) as upstream_total_count, SUM(responses_bootstrap_recovery_count) as responses_bootstrap_recovery_count, SUM(responses_bootstrap_recovery_success_count) as responses_bootstrap_recovery_success_count, SUM(responses_bootstrap_recovery_wait_ms) as responses_bootstrap_recovery_wait_ms").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		query = query.Where(commonGroupCol+" IN ?", groups)
	}
	err := query.
		Group("model_name").
		Having("SUM(request_count) > 0 OR SUM(responses_bootstrap_recovery_count) > 0").
		Find(&summaries).Error
	return summaries, err
}

func DeletePerfMetricsBefore(cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return DB.Where("bucket_ts < ?", cutoffTs).Delete(&PerfMetric{}).Error
}

func PerfMetricStartTime(hours int) int64 {
	if hours <= 0 {
		hours = 24
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
}
