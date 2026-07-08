package perfmetrics

import "sync/atomic"

type Store interface {
	Record(sample Sample)
	Query(params QueryParams) (QueryResult, error)
}

type Sample struct {
	Model                               string
	Group                               string
	LatencyMs                           int64
	TtftMs                              int64
	HasTtft                             bool
	Success                             bool
	BootstrapOnly                       bool
	OutputTokens                        int64
	GenerationMs                        int64
	UpstreamHeaderMs                    int64
	HasUpstreamHeader                   bool
	UpstreamTtfbMs                      int64
	HasUpstreamTtfb                     bool
	UpstreamTotalMs                     int64
	HasUpstreamTotal                    bool
	ResponsesBootstrapRecoveryAttempted bool
	ResponsesBootstrapRecoveryWaitMs    int64
}

type QueryParams struct {
	Model string
	Group string
	Hours int
}

type BucketPoint struct {
	Ts                                     int64   `json:"ts"`
	AvgTtftMs                              int64   `json:"avg_ttft_ms"`
	AvgLatencyMs                           int64   `json:"avg_latency_ms"`
	AvgUpstreamHeaderMs                    int64   `json:"avg_upstream_header_ms"`
	AvgUpstreamTtfbMs                      int64   `json:"avg_upstream_ttfb_ms"`
	AvgUpstreamTotalMs                     int64   `json:"avg_upstream_total_ms"`
	AvgResponsesBootstrapRecoveryWaitMs    int64   `json:"avg_responses_bootstrap_recovery_wait_ms"`
	SuccessRate                            float64 `json:"success_rate"`
	ResponsesBootstrapRecoverySuccessRate  float64 `json:"responses_bootstrap_recovery_success_rate"`
	Count                                  int64   `json:"count"`
	SuccessCount                           int64   `json:"success_count"`
	TtftCount                              int64   `json:"ttft_count"`
	UpstreamHeaderCount                    int64   `json:"upstream_header_count"`
	UpstreamTtfbCount                      int64   `json:"upstream_ttfb_count"`
	UpstreamTotalCount                     int64   `json:"upstream_total_count"`
	ResponsesBootstrapRecoveryCount        int64   `json:"responses_bootstrap_recovery_count"`
	ResponsesBootstrapRecoverySuccessCount int64   `json:"responses_bootstrap_recovery_success_count"`
}

type GroupResult struct {
	Group                                  string        `json:"group"`
	AvgTtftMs                              int64         `json:"avg_ttft_ms"`
	AvgLatencyMs                           int64         `json:"avg_latency_ms"`
	AvgUpstreamHeaderMs                    int64         `json:"avg_upstream_header_ms"`
	AvgUpstreamTtfbMs                      int64         `json:"avg_upstream_ttfb_ms"`
	AvgUpstreamTotalMs                     int64         `json:"avg_upstream_total_ms"`
	AvgResponsesBootstrapRecoveryWaitMs    int64         `json:"avg_responses_bootstrap_recovery_wait_ms"`
	SuccessRate                            float64       `json:"success_rate"`
	ResponsesBootstrapRecoverySuccessRate  float64       `json:"responses_bootstrap_recovery_success_rate"`
	RequestCount                           int64         `json:"request_count"`
	SuccessCount                           int64         `json:"success_count"`
	TtftCount                              int64         `json:"ttft_count"`
	UpstreamHeaderCount                    int64         `json:"upstream_header_count"`
	UpstreamTtfbCount                      int64         `json:"upstream_ttfb_count"`
	UpstreamTotalCount                     int64         `json:"upstream_total_count"`
	ResponsesBootstrapRecoveryCount        int64         `json:"responses_bootstrap_recovery_count"`
	ResponsesBootstrapRecoverySuccessCount int64         `json:"responses_bootstrap_recovery_success_count"`
	Series                                 []BucketPoint `json:"series"`
}

type QueryResult struct {
	ModelName    string        `json:"model_name"`
	SeriesSchema string        `json:"series_schema"`
	Groups       []GroupResult `json:"groups"`
}

type ModelSummary struct {
	ModelName                              string  `json:"model_name"`
	AvgLatencyMs                           int64   `json:"avg_latency_ms"`
	AvgUpstreamHeaderMs                    int64   `json:"avg_upstream_header_ms"`
	AvgUpstreamTtfbMs                      int64   `json:"avg_upstream_ttfb_ms"`
	AvgUpstreamTotalMs                     int64   `json:"avg_upstream_total_ms"`
	AvgResponsesBootstrapRecoveryWaitMs    int64   `json:"avg_responses_bootstrap_recovery_wait_ms"`
	SuccessRate                            float64 `json:"success_rate"`
	ResponsesBootstrapRecoverySuccessRate  float64 `json:"responses_bootstrap_recovery_success_rate"`
	AvgTps                                 float64 `json:"avg_tps"`
	RequestCount                           int64   `json:"request_count"`
	UpstreamHeaderCount                    int64   `json:"upstream_header_count"`
	UpstreamTtfbCount                      int64   `json:"upstream_ttfb_count"`
	UpstreamTotalCount                     int64   `json:"upstream_total_count"`
	ResponsesBootstrapRecoveryCount        int64   `json:"responses_bootstrap_recovery_count"`
	ResponsesBootstrapRecoverySuccessCount int64   `json:"responses_bootstrap_recovery_success_count"`
}

type SummaryAllResult struct {
	Models []ModelSummary `json:"models"`
}

type bucketKey struct {
	model    string
	group    string
	bucketTs int64
}

type counters struct {
	requestCount                           int64
	successCount                           int64
	totalLatencyMs                         int64
	ttftSumMs                              int64
	ttftCount                              int64
	outputTokens                           int64
	generationMs                           int64
	upstreamHeaderMs                       int64
	upstreamHeaderCount                    int64
	upstreamTtfbMs                         int64
	upstreamTtfbCount                      int64
	upstreamTotalMs                        int64
	upstreamTotalCount                     int64
	responsesBootstrapRecoveryCount        int64
	responsesBootstrapRecoverySuccessCount int64
	responsesBootstrapRecoveryWaitMs       int64
}

type atomicBucket struct {
	requestCount                           atomic.Int64
	successCount                           atomic.Int64
	totalLatencyMs                         atomic.Int64
	ttftSumMs                              atomic.Int64
	ttftCount                              atomic.Int64
	outputTokens                           atomic.Int64
	generationMs                           atomic.Int64
	upstreamHeaderMs                       atomic.Int64
	upstreamHeaderCount                    atomic.Int64
	upstreamTtfbMs                         atomic.Int64
	upstreamTtfbCount                      atomic.Int64
	upstreamTotalMs                        atomic.Int64
	upstreamTotalCount                     atomic.Int64
	responsesBootstrapRecoveryCount        atomic.Int64
	responsesBootstrapRecoverySuccessCount atomic.Int64
	responsesBootstrapRecoveryWaitMs       atomic.Int64
}

func (b *atomicBucket) add(sample Sample) {
	if !sample.BootstrapOnly {
		b.requestCount.Add(1)
		if sample.Success {
			b.successCount.Add(1)
		}
		if sample.LatencyMs > 0 {
			b.totalLatencyMs.Add(sample.LatencyMs)
		}
		if sample.HasTtft && sample.TtftMs >= 0 {
			b.ttftSumMs.Add(sample.TtftMs)
			b.ttftCount.Add(1)
		}
		if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
			b.outputTokens.Add(sample.OutputTokens)
			b.generationMs.Add(sample.GenerationMs)
		}
		if sample.HasUpstreamHeader && sample.UpstreamHeaderMs >= 0 {
			b.upstreamHeaderMs.Add(sample.UpstreamHeaderMs)
			b.upstreamHeaderCount.Add(1)
		}
		if sample.HasUpstreamTtfb && sample.UpstreamTtfbMs >= 0 {
			b.upstreamTtfbMs.Add(sample.UpstreamTtfbMs)
			b.upstreamTtfbCount.Add(1)
		}
		if sample.HasUpstreamTotal && sample.UpstreamTotalMs >= 0 {
			b.upstreamTotalMs.Add(sample.UpstreamTotalMs)
			b.upstreamTotalCount.Add(1)
		}
	}
	if sample.ResponsesBootstrapRecoveryAttempted {
		b.responsesBootstrapRecoveryCount.Add(1)
		if sample.Success {
			b.responsesBootstrapRecoverySuccessCount.Add(1)
		}
		if sample.ResponsesBootstrapRecoveryWaitMs > 0 {
			b.responsesBootstrapRecoveryWaitMs.Add(sample.ResponsesBootstrapRecoveryWaitMs)
		}
	}
}

func (b *atomicBucket) snapshot() counters {
	return counters{
		requestCount:                           b.requestCount.Load(),
		successCount:                           b.successCount.Load(),
		totalLatencyMs:                         b.totalLatencyMs.Load(),
		ttftSumMs:                              b.ttftSumMs.Load(),
		ttftCount:                              b.ttftCount.Load(),
		outputTokens:                           b.outputTokens.Load(),
		generationMs:                           b.generationMs.Load(),
		upstreamHeaderMs:                       b.upstreamHeaderMs.Load(),
		upstreamHeaderCount:                    b.upstreamHeaderCount.Load(),
		upstreamTtfbMs:                         b.upstreamTtfbMs.Load(),
		upstreamTtfbCount:                      b.upstreamTtfbCount.Load(),
		upstreamTotalMs:                        b.upstreamTotalMs.Load(),
		upstreamTotalCount:                     b.upstreamTotalCount.Load(),
		responsesBootstrapRecoveryCount:        b.responsesBootstrapRecoveryCount.Load(),
		responsesBootstrapRecoverySuccessCount: b.responsesBootstrapRecoverySuccessCount.Load(),
		responsesBootstrapRecoveryWaitMs:       b.responsesBootstrapRecoveryWaitMs.Load(),
	}
}

func (b *atomicBucket) drain() counters {
	return counters{
		requestCount:                           b.requestCount.Swap(0),
		successCount:                           b.successCount.Swap(0),
		totalLatencyMs:                         b.totalLatencyMs.Swap(0),
		ttftSumMs:                              b.ttftSumMs.Swap(0),
		ttftCount:                              b.ttftCount.Swap(0),
		outputTokens:                           b.outputTokens.Swap(0),
		generationMs:                           b.generationMs.Swap(0),
		upstreamHeaderMs:                       b.upstreamHeaderMs.Swap(0),
		upstreamHeaderCount:                    b.upstreamHeaderCount.Swap(0),
		upstreamTtfbMs:                         b.upstreamTtfbMs.Swap(0),
		upstreamTtfbCount:                      b.upstreamTtfbCount.Swap(0),
		upstreamTotalMs:                        b.upstreamTotalMs.Swap(0),
		upstreamTotalCount:                     b.upstreamTotalCount.Swap(0),
		responsesBootstrapRecoveryCount:        b.responsesBootstrapRecoveryCount.Swap(0),
		responsesBootstrapRecoverySuccessCount: b.responsesBootstrapRecoverySuccessCount.Swap(0),
		responsesBootstrapRecoveryWaitMs:       b.responsesBootstrapRecoveryWaitMs.Swap(0),
	}
}

func (b *atomicBucket) addCounters(c counters) {
	if c.requestCount != 0 {
		b.requestCount.Add(c.requestCount)
	}
	if c.successCount != 0 {
		b.successCount.Add(c.successCount)
	}
	if c.totalLatencyMs != 0 {
		b.totalLatencyMs.Add(c.totalLatencyMs)
	}
	if c.ttftSumMs != 0 {
		b.ttftSumMs.Add(c.ttftSumMs)
	}
	if c.ttftCount != 0 {
		b.ttftCount.Add(c.ttftCount)
	}
	if c.outputTokens != 0 {
		b.outputTokens.Add(c.outputTokens)
	}
	if c.generationMs != 0 {
		b.generationMs.Add(c.generationMs)
	}
	if c.upstreamHeaderMs != 0 {
		b.upstreamHeaderMs.Add(c.upstreamHeaderMs)
	}
	if c.upstreamHeaderCount != 0 {
		b.upstreamHeaderCount.Add(c.upstreamHeaderCount)
	}
	if c.upstreamTtfbMs != 0 {
		b.upstreamTtfbMs.Add(c.upstreamTtfbMs)
	}
	if c.upstreamTtfbCount != 0 {
		b.upstreamTtfbCount.Add(c.upstreamTtfbCount)
	}
	if c.upstreamTotalMs != 0 {
		b.upstreamTotalMs.Add(c.upstreamTotalMs)
	}
	if c.upstreamTotalCount != 0 {
		b.upstreamTotalCount.Add(c.upstreamTotalCount)
	}
	if c.responsesBootstrapRecoveryCount != 0 {
		b.responsesBootstrapRecoveryCount.Add(c.responsesBootstrapRecoveryCount)
	}
	if c.responsesBootstrapRecoverySuccessCount != 0 {
		b.responsesBootstrapRecoverySuccessCount.Add(c.responsesBootstrapRecoverySuccessCount)
	}
	if c.responsesBootstrapRecoveryWaitMs != 0 {
		b.responsesBootstrapRecoveryWaitMs.Add(c.responsesBootstrapRecoveryWaitMs)
	}
}
