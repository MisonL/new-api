package perfmetrics

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

var hotBuckets = &sync.Map{}

const seriesSchema = "b4f2a691c7d03e58"

func Init() {
	go flushLoop()
}

func RecordRelaySample(info *relaycommon.RelayInfo, success bool, outputTokens int64) {
	if info == nil {
		return
	}
	now := time.Now()
	hasTtft := info.IsStream && info.HasSendResponse()
	ttftMs := int64(0)
	if hasTtft {
		ttftMs, hasTtft = info.FirstResponseLatencyMs()
	}
	latencyMs := now.Sub(info.StartTime).Milliseconds()
	generationMs := latencyMs
	if hasTtft {
		generationMs = now.Sub(info.FirstResponseTime).Milliseconds()
	}
	if generationMs <= 0 {
		generationMs = latencyMs
	}
	upstreamHeaderMs, hasUpstreamHeader := info.UpstreamHeaderLatencyMs()
	upstreamTtfbMs, hasUpstreamTtfb := info.UpstreamFirstByteLatencyMs()
	upstreamTotalMs, hasUpstreamTotal := info.UpstreamTotalLatencyMs()
	Record(Sample{
		Model:                               info.OriginModelName,
		Group:                               info.UsingGroup,
		LatencyMs:                           latencyMs,
		TtftMs:                              ttftMs,
		HasTtft:                             hasTtft,
		Success:                             success,
		OutputTokens:                        outputTokens,
		GenerationMs:                        generationMs,
		UpstreamHeaderMs:                    upstreamHeaderMs,
		HasUpstreamHeader:                   hasUpstreamHeader,
		UpstreamTtfbMs:                      upstreamTtfbMs,
		HasUpstreamTtfb:                     hasUpstreamTtfb,
		UpstreamTotalMs:                     upstreamTotalMs,
		HasUpstreamTotal:                    hasUpstreamTotal,
		ResponsesBootstrapRecoveryAttempted: info.ResponsesBootstrapRecoveryAttempted,
		ResponsesBootstrapRecoveryWaitMs:    info.ResponsesBootstrapRecoveryWaitMs,
	})
}

func Record(sample Sample) {
	setting := perf_metrics_setting.GetSetting()
	if !setting.Enabled || sample.Model == "" {
		return
	}
	if sample.Group == "" {
		sample.Group = "default"
	}
	if sample.LatencyMs < 0 {
		sample.LatencyMs = 0
	}

	key := bucketKey{
		model:    sample.Model,
		group:    sample.Group,
		bucketTs: bucketStart(time.Now().Unix()),
	}
	actual, _ := hotBuckets.LoadOrStore(key, &atomicBucket{})
	actual.(*atomicBucket).add(sample)
	recordRedis(key, sample)
}

func Query(params QueryParams) (QueryResult, error) {
	if params.Hours <= 0 {
		params.Hours = 24
	}
	if params.Hours > 24*30 {
		params.Hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(params.Hours)*3600

	merged := map[bucketKey]counters{}
	rows, err := model.GetPerfMetrics(params.Model, params.Group, startTs, endTs)
	if err != nil {
		return QueryResult{}, err
	}
	for _, row := range rows {
		mergeCounters(merged, bucketKey{
			model:    row.ModelName,
			group:    row.Group,
			bucketTs: row.BucketTs,
		}, counters{
			requestCount:                           row.RequestCount,
			successCount:                           row.SuccessCount,
			totalLatencyMs:                         row.TotalLatencyMs,
			ttftSumMs:                              row.TtftSumMs,
			ttftCount:                              row.TtftCount,
			outputTokens:                           row.OutputTokens,
			generationMs:                           row.GenerationMs,
			upstreamHeaderMs:                       row.UpstreamHeaderMs,
			upstreamHeaderCount:                    row.UpstreamHeaderCount,
			upstreamTtfbMs:                         row.UpstreamTtfbMs,
			upstreamTtfbCount:                      row.UpstreamTtfbCount,
			upstreamTotalMs:                        row.UpstreamTotalMs,
			upstreamTotalCount:                     row.UpstreamTotalCount,
			responsesBootstrapRecoveryCount:        row.ResponsesBootstrapRecoveryCount,
			responsesBootstrapRecoverySuccessCount: row.ResponsesBootstrapRecoverySuccessCount,
			responsesBootstrapRecoveryWaitMs:       row.ResponsesBootstrapRecoveryWaitMs,
		})
	}

	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.model != params.Model || k.bucketTs < startTs || k.bucketTs > endTs {
			return true
		}
		if params.Group != "" && k.group != params.Group {
			return true
		}
		mergeCounters(merged, k, value.(*atomicBucket).snapshot())
		return true
	})

	return buildQueryResult(params.Model, merged), nil
}

func QuerySummaryAll(hours int, groups []string) (SummaryAllResult, error) {
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600
	allowedGroups := allowedGroupSet(groups)

	rows, err := model.GetPerfMetricsSummaryAll(startTs, endTs, groups)
	if err != nil {
		return SummaryAllResult{}, err
	}

	totals := map[string]counters{}
	for _, row := range rows {
		totals[row.ModelName] = counters{
			requestCount:                           row.RequestCount,
			successCount:                           row.SuccessCount,
			totalLatencyMs:                         row.TotalLatencyMs,
			outputTokens:                           row.OutputTokens,
			generationMs:                           row.GenerationMs,
			upstreamHeaderMs:                       row.UpstreamHeaderMs,
			upstreamHeaderCount:                    row.UpstreamHeaderCount,
			upstreamTtfbMs:                         row.UpstreamTtfbMs,
			upstreamTtfbCount:                      row.UpstreamTtfbCount,
			upstreamTotalMs:                        row.UpstreamTotalMs,
			upstreamTotalCount:                     row.UpstreamTotalCount,
			responsesBootstrapRecoveryCount:        row.ResponsesBootstrapRecoveryCount,
			responsesBootstrapRecoverySuccessCount: row.ResponsesBootstrapRecoverySuccessCount,
			responsesBootstrapRecoveryWaitMs:       row.ResponsesBootstrapRecoveryWaitMs,
		}
	}

	mergeHotBucketSummaries(totals, startTs, endTs, allowedGroups)

	models := make([]ModelSummary, 0, len(totals))
	for name, total := range totals {
		if countersEmpty(total) {
			continue
		}
		models = append(models, ModelSummary{
			ModelName:                              name,
			AvgLatencyMs:                           avg(total.totalLatencyMs, total.requestCount),
			AvgUpstreamHeaderMs:                    avg(total.upstreamHeaderMs, total.upstreamHeaderCount),
			AvgUpstreamTtfbMs:                      avg(total.upstreamTtfbMs, total.upstreamTtfbCount),
			AvgUpstreamTotalMs:                     avg(total.upstreamTotalMs, total.upstreamTotalCount),
			AvgResponsesBootstrapRecoveryWaitMs:    avg(total.responsesBootstrapRecoveryWaitMs, total.responsesBootstrapRecoveryCount),
			SuccessRate:                            math.Round(successRate(total)*100) / 100,
			ResponsesBootstrapRecoverySuccessRate:  math.Round(responsesBootstrapRecoverySuccessRate(total)*100) / 100,
			AvgTps:                                 math.Round(avgTps(total)*100) / 100,
			RequestCount:                           total.requestCount,
			UpstreamHeaderCount:                    total.upstreamHeaderCount,
			UpstreamTtfbCount:                      total.upstreamTtfbCount,
			UpstreamTotalCount:                     total.upstreamTotalCount,
			ResponsesBootstrapRecoveryCount:        total.responsesBootstrapRecoveryCount,
			ResponsesBootstrapRecoverySuccessCount: total.responsesBootstrapRecoverySuccessCount,
		})
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].ModelName < models[j].ModelName
	})

	return SummaryAllResult{Models: models}, nil
}

func mergeHotBucketSummaries(totals map[string]counters, startTs int64, endTs int64, allowedGroups map[string]struct{}) {
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.bucketTs < startTs || k.bucketTs > endTs {
			return true
		}
		if allowedGroups != nil {
			if _, ok := allowedGroups[k.group]; !ok {
				return true
			}
		}
		snap := value.(*atomicBucket).snapshot()
		if countersEmpty(snap) {
			return true
		}
		cur := totals[k.model]
		cur.requestCount += snap.requestCount
		cur.successCount += snap.successCount
		cur.totalLatencyMs += snap.totalLatencyMs
		cur.outputTokens += snap.outputTokens
		cur.generationMs += snap.generationMs
		cur.upstreamHeaderMs += snap.upstreamHeaderMs
		cur.upstreamHeaderCount += snap.upstreamHeaderCount
		cur.upstreamTtfbMs += snap.upstreamTtfbMs
		cur.upstreamTtfbCount += snap.upstreamTtfbCount
		cur.upstreamTotalMs += snap.upstreamTotalMs
		cur.upstreamTotalCount += snap.upstreamTotalCount
		cur.responsesBootstrapRecoveryCount += snap.responsesBootstrapRecoveryCount
		cur.responsesBootstrapRecoverySuccessCount += snap.responsesBootstrapRecoverySuccessCount
		cur.responsesBootstrapRecoveryWaitMs += snap.responsesBootstrapRecoveryWaitMs
		totals[k.model] = cur
		return true
	})
}

func allowedGroupSet(groups []string) map[string]struct{} {
	if groups == nil {
		return nil
	}
	allowed := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		allowed[group] = struct{}{}
	}
	return allowed
}

func bucketStart(ts int64) int64 {
	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	return ts - (ts % bucketSeconds)
}

func mergeCounters(merged map[bucketKey]counters, key bucketKey, value counters) {
	if countersEmpty(value) {
		return
	}
	current := merged[key]
	current.requestCount += value.requestCount
	current.successCount += value.successCount
	current.totalLatencyMs += value.totalLatencyMs
	current.ttftSumMs += value.ttftSumMs
	current.ttftCount += value.ttftCount
	current.outputTokens += value.outputTokens
	current.generationMs += value.generationMs
	current.upstreamHeaderMs += value.upstreamHeaderMs
	current.upstreamHeaderCount += value.upstreamHeaderCount
	current.upstreamTtfbMs += value.upstreamTtfbMs
	current.upstreamTtfbCount += value.upstreamTtfbCount
	current.upstreamTotalMs += value.upstreamTotalMs
	current.upstreamTotalCount += value.upstreamTotalCount
	current.responsesBootstrapRecoveryCount += value.responsesBootstrapRecoveryCount
	current.responsesBootstrapRecoverySuccessCount += value.responsesBootstrapRecoverySuccessCount
	current.responsesBootstrapRecoveryWaitMs += value.responsesBootstrapRecoveryWaitMs
	merged[key] = current
}

func buildQueryResult(modelName string, merged map[bucketKey]counters) QueryResult {
	groupBuckets := map[string]map[int64]counters{}
	for key, value := range merged {
		if countersEmpty(value) {
			continue
		}
		if _, ok := groupBuckets[key.group]; !ok {
			groupBuckets[key.group] = map[int64]counters{}
		}
		groupBuckets[key.group][key.bucketTs] = value
	}

	groups := make([]string, 0, len(groupBuckets))
	for group := range groupBuckets {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	results := make([]GroupResult, 0, len(groups))
	for _, group := range groups {
		buckets := groupBuckets[group]
		timestamps := make([]int64, 0, len(buckets))
		for ts := range buckets {
			timestamps = append(timestamps, ts)
		}
		sort.Slice(timestamps, func(i, j int) bool {
			return timestamps[i] < timestamps[j]
		})

		total := counters{}
		series := make([]BucketPoint, 0, len(timestamps))
		for _, ts := range timestamps {
			value := buckets[ts]
			total.requestCount += value.requestCount
			total.successCount += value.successCount
			total.totalLatencyMs += value.totalLatencyMs
			total.ttftSumMs += value.ttftSumMs
			total.ttftCount += value.ttftCount
			total.outputTokens += value.outputTokens
			total.generationMs += value.generationMs
			total.upstreamHeaderMs += value.upstreamHeaderMs
			total.upstreamHeaderCount += value.upstreamHeaderCount
			total.upstreamTtfbMs += value.upstreamTtfbMs
			total.upstreamTtfbCount += value.upstreamTtfbCount
			total.upstreamTotalMs += value.upstreamTotalMs
			total.upstreamTotalCount += value.upstreamTotalCount
			total.responsesBootstrapRecoveryCount += value.responsesBootstrapRecoveryCount
			total.responsesBootstrapRecoverySuccessCount += value.responsesBootstrapRecoverySuccessCount
			total.responsesBootstrapRecoveryWaitMs += value.responsesBootstrapRecoveryWaitMs
			series = append(series, bucketPoint(ts, value))
		}

		results = append(results, GroupResult{
			Group:                                  group,
			AvgTtftMs:                              avg(total.ttftSumMs, total.ttftCount),
			AvgLatencyMs:                           avg(total.totalLatencyMs, total.requestCount),
			AvgUpstreamHeaderMs:                    avg(total.upstreamHeaderMs, total.upstreamHeaderCount),
			AvgUpstreamTtfbMs:                      avg(total.upstreamTtfbMs, total.upstreamTtfbCount),
			AvgUpstreamTotalMs:                     avg(total.upstreamTotalMs, total.upstreamTotalCount),
			AvgResponsesBootstrapRecoveryWaitMs:    avg(total.responsesBootstrapRecoveryWaitMs, total.responsesBootstrapRecoveryCount),
			SuccessRate:                            successRate(total),
			ResponsesBootstrapRecoverySuccessRate:  responsesBootstrapRecoverySuccessRate(total),
			RequestCount:                           total.requestCount,
			SuccessCount:                           total.successCount,
			TtftCount:                              total.ttftCount,
			UpstreamHeaderCount:                    total.upstreamHeaderCount,
			UpstreamTtfbCount:                      total.upstreamTtfbCount,
			UpstreamTotalCount:                     total.upstreamTotalCount,
			ResponsesBootstrapRecoveryCount:        total.responsesBootstrapRecoveryCount,
			ResponsesBootstrapRecoverySuccessCount: total.responsesBootstrapRecoverySuccessCount,
			Series:                                 series,
		})
	}

	return QueryResult{
		ModelName:    modelName,
		SeriesSchema: seriesSchema,
		Groups:       results,
	}
}

func bucketPoint(ts int64, value counters) BucketPoint {
	return BucketPoint{
		Ts:                                     ts,
		AvgTtftMs:                              avg(value.ttftSumMs, value.ttftCount),
		AvgLatencyMs:                           avg(value.totalLatencyMs, value.requestCount),
		AvgUpstreamHeaderMs:                    avg(value.upstreamHeaderMs, value.upstreamHeaderCount),
		AvgUpstreamTtfbMs:                      avg(value.upstreamTtfbMs, value.upstreamTtfbCount),
		AvgUpstreamTotalMs:                     avg(value.upstreamTotalMs, value.upstreamTotalCount),
		AvgResponsesBootstrapRecoveryWaitMs:    avg(value.responsesBootstrapRecoveryWaitMs, value.responsesBootstrapRecoveryCount),
		SuccessRate:                            successRate(value),
		ResponsesBootstrapRecoverySuccessRate:  responsesBootstrapRecoverySuccessRate(value),
		Count:                                  value.requestCount,
		SuccessCount:                           value.successCount,
		TtftCount:                              value.ttftCount,
		UpstreamHeaderCount:                    value.upstreamHeaderCount,
		UpstreamTtfbCount:                      value.upstreamTtfbCount,
		UpstreamTotalCount:                     value.upstreamTotalCount,
		ResponsesBootstrapRecoveryCount:        value.responsesBootstrapRecoveryCount,
		ResponsesBootstrapRecoverySuccessCount: value.responsesBootstrapRecoverySuccessCount,
	}
}

func avg(sum int64, count int64) int64 {
	if count <= 0 {
		return 0
	}
	return sum / count
}

func successRate(value counters) float64 {
	if value.requestCount <= 0 {
		return 0
	}
	return float64(value.successCount) / float64(value.requestCount) * 100
}

func responsesBootstrapRecoverySuccessRate(value counters) float64 {
	if value.responsesBootstrapRecoveryCount <= 0 {
		return 0
	}
	return float64(value.responsesBootstrapRecoverySuccessCount) / float64(value.responsesBootstrapRecoveryCount) * 100
}

func countersEmpty(value counters) bool {
	return value == counters{}
}

func avgTps(value counters) float64 {
	if value.outputTokens <= 0 || value.generationMs <= 0 {
		return 0
	}
	return float64(value.outputTokens) / (float64(value.generationMs) / 1000)
}

func recordRedis(key bucketKey, sample Sample) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	redisKey := redisBucketKey(key)
	pipe := common.RDB.TxPipeline()
	if !sample.BootstrapOnly {
		pipe.HIncrBy(ctx, redisKey, "req", 1)
		if sample.Success {
			pipe.HIncrBy(ctx, redisKey, "ok", 1)
		}
		if sample.LatencyMs > 0 {
			pipe.HIncrBy(ctx, redisKey, "lat", sample.LatencyMs)
		}
		if sample.HasTtft && sample.TtftMs >= 0 {
			pipe.HIncrBy(ctx, redisKey, "ttft", sample.TtftMs)
			pipe.HIncrBy(ctx, redisKey, "ttft_n", 1)
		}
		if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
			pipe.HIncrBy(ctx, redisKey, "out", sample.OutputTokens)
			pipe.HIncrBy(ctx, redisKey, "gen_ms", sample.GenerationMs)
		}
		if sample.HasUpstreamHeader && sample.UpstreamHeaderMs >= 0 {
			pipe.HIncrBy(ctx, redisKey, "up_hdr", sample.UpstreamHeaderMs)
			pipe.HIncrBy(ctx, redisKey, "up_hdr_n", 1)
		}
		if sample.HasUpstreamTtfb && sample.UpstreamTtfbMs >= 0 {
			pipe.HIncrBy(ctx, redisKey, "up_ttfb", sample.UpstreamTtfbMs)
			pipe.HIncrBy(ctx, redisKey, "up_ttfb_n", 1)
		}
		if sample.HasUpstreamTotal && sample.UpstreamTotalMs >= 0 {
			pipe.HIncrBy(ctx, redisKey, "up_total", sample.UpstreamTotalMs)
			pipe.HIncrBy(ctx, redisKey, "up_total_n", 1)
		}
	}
	if sample.ResponsesBootstrapRecoveryAttempted {
		pipe.HIncrBy(ctx, redisKey, "rb", 1)
		if sample.Success {
			pipe.HIncrBy(ctx, redisKey, "rb_ok", 1)
		}
		if sample.ResponsesBootstrapRecoveryWaitMs > 0 {
			pipe.HIncrBy(ctx, redisKey, "rb_wait", sample.ResponsesBootstrapRecoveryWaitMs)
		}
	}
	pipe.Expire(ctx, redisKey, time.Hour)
	_, _ = pipe.Exec(ctx)
}

func mergeRedisActiveBuckets(merged map[bucketKey]counters, params QueryParams, startTs int64, endTs int64) {
	if !common.RedisEnabled || common.RDB == nil || params.Model == "" || params.Group == "" {
		return
	}
	active := bucketStart(time.Now().Unix())
	if active < startTs || active > endTs {
		return
	}
	key := bucketKey{model: params.Model, group: params.Group, bucketTs: active}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	values, err := common.RDB.HGetAll(ctx, redisBucketKey(key)).Result()
	if err != nil || len(values) == 0 {
		return
	}
	mergeCounters(merged, key, redisCounters(values))
}

func redisBucketKey(key bucketKey) string {
	return fmt.Sprintf("perf:%s:%s:%d", key.model, key.group, key.bucketTs)
}
