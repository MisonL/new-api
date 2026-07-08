package perfmetrics

import (
	"sync"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestAllowedGroupSet(t *testing.T) {
	require.Nil(t, allowedGroupSet(nil))

	empty := allowedGroupSet([]string{})
	require.NotNil(t, empty)
	require.Empty(t, empty)

	allowed := allowedGroupSet([]string{"default", "auto"})
	_, hasDefault := allowed["default"]
	_, hasAuto := allowed["auto"]
	_, hasLegacy := allowed["legacy"]
	require.True(t, hasDefault)
	require.True(t, hasAuto)
	require.False(t, hasLegacy)
}

func TestMergeHotBucketSummariesFiltersByGroup(t *testing.T) {
	hotBuckets = syncMapForTest(t)
	bucketTs := bucketStart(time.Now().Unix())
	legacyBucket := &atomicBucket{}
	legacyBucket.add(Sample{
		Model:     "gpt-test",
		Group:     "legacy",
		LatencyMs: 900,
		Success:   true,
	})
	defaultBucket := &atomicBucket{}
	defaultBucket.add(Sample{
		Model:             "gpt-test",
		Group:             "default",
		LatencyMs:         100,
		Success:           true,
		UpstreamHeaderMs:  25,
		HasUpstreamHeader: true,
		UpstreamTtfbMs:    30,
		HasUpstreamTtfb:   true,
		UpstreamTotalMs:   70,
		HasUpstreamTotal:  true,
	})

	hotBuckets.Store(bucketKey{model: "gpt-test", group: "legacy", bucketTs: bucketTs}, legacyBucket)
	hotBuckets.Store(bucketKey{model: "gpt-test", group: "default", bucketTs: bucketTs}, defaultBucket)

	totals := map[string]counters{}
	mergeHotBucketSummaries(totals, bucketTs-1, bucketTs+1, allowedGroupSet([]string{"default"}))

	require.Len(t, totals, 1)
	require.Equal(t, int64(1), totals["gpt-test"].requestCount)
	require.Equal(t, int64(100), totals["gpt-test"].totalLatencyMs)
	require.Equal(t, int64(25), totals["gpt-test"].upstreamHeaderMs)
	require.Equal(t, int64(30), totals["gpt-test"].upstreamTtfbMs)
	require.Equal(t, int64(70), totals["gpt-test"].upstreamTotalMs)
	require.Equal(t, int64(1), totals["gpt-test"].upstreamHeaderCount)
	require.Equal(t, int64(1), totals["gpt-test"].upstreamTtfbCount)
	require.Equal(t, int64(1), totals["gpt-test"].upstreamTotalCount)
}

func TestBootstrapOnlySampleDoesNotIncrementRequestCount(t *testing.T) {
	bucket := &atomicBucket{}
	bucket.add(Sample{
		Model:                               "gpt-test",
		Group:                               "default",
		Success:                             false,
		BootstrapOnly:                       true,
		ResponsesBootstrapRecoveryAttempted: true,
		ResponsesBootstrapRecoveryWaitMs:    1500,
	})

	snapshot := bucket.snapshot()
	require.Equal(t, int64(0), snapshot.requestCount)
	require.Equal(t, int64(0), snapshot.successCount)
	require.Equal(t, int64(1), snapshot.responsesBootstrapRecoveryCount)
	require.Equal(t, int64(0), snapshot.responsesBootstrapRecoverySuccessCount)
	require.Equal(t, int64(1500), snapshot.responsesBootstrapRecoveryWaitMs)
}

func TestRecordBootstrapOnlySampleThroughPublicEntryPoint(t *testing.T) {
	hotBuckets = syncMapForTest(t)

	Record(Sample{
		Model:                               "gpt-test",
		Group:                               "default",
		BootstrapOnly:                       true,
		ResponsesBootstrapRecoveryAttempted: true,
		ResponsesBootstrapRecoveryWaitMs:    1500,
	})

	var snapshot counters
	var found bool
	hotBuckets.Range(func(_, value any) bool {
		snapshot = value.(*atomicBucket).snapshot()
		found = true
		return false
	})
	require.True(t, found)
	require.Equal(t, int64(0), snapshot.requestCount)
	require.Equal(t, int64(0), snapshot.successCount)
	require.Equal(t, int64(1), snapshot.responsesBootstrapRecoveryCount)
	require.Equal(t, int64(0), snapshot.responsesBootstrapRecoverySuccessCount)
	require.Equal(t, int64(1500), snapshot.responsesBootstrapRecoveryWaitMs)
}

func TestRecordRelaySampleIncludesBootstrapRecoveryMetric(t *testing.T) {
	hotBuckets = syncMapForTest(t)

	RecordRelaySample(&relaycommon.RelayInfo{
		OriginModelName:                     "gpt-test",
		UsingGroup:                          "default",
		StartTime:                           time.Now().Add(-2 * time.Second),
		ResponsesBootstrapRecoveryAttempted: true,
		ResponsesBootstrapRecoveryWaitMs:    750,
	}, false, 0)

	var snapshot counters
	var found bool
	hotBuckets.Range(func(_, value any) bool {
		snapshot = value.(*atomicBucket).snapshot()
		found = true
		return false
	})
	require.True(t, found)
	require.Equal(t, int64(1), snapshot.requestCount)
	require.Equal(t, int64(0), snapshot.successCount)
	require.Equal(t, int64(1), snapshot.responsesBootstrapRecoveryCount)
	require.Equal(t, int64(0), snapshot.responsesBootstrapRecoverySuccessCount)
	require.Equal(t, int64(750), snapshot.responsesBootstrapRecoveryWaitMs)
}

func TestMergeCountersPreservesBootstrapRecoveryFields(t *testing.T) {
	key := bucketKey{model: "gpt-test", group: "default", bucketTs: bucketStart(time.Now().Unix())}
	merged := map[bucketKey]counters{}

	mergeCounters(merged, key, counters{
		requestCount:                           1,
		successCount:                           1,
		responsesBootstrapRecoveryCount:        1,
		responsesBootstrapRecoverySuccessCount: 1,
		responsesBootstrapRecoveryWaitMs:       500,
	})
	mergeCounters(merged, key, counters{
		requestCount:                     2,
		responsesBootstrapRecoveryCount:  3,
		responsesBootstrapRecoveryWaitMs: 1500,
	})

	snapshot := merged[key]
	require.Equal(t, int64(3), snapshot.requestCount)
	require.Equal(t, int64(1), snapshot.successCount)
	require.Equal(t, int64(4), snapshot.responsesBootstrapRecoveryCount)
	require.Equal(t, int64(1), snapshot.responsesBootstrapRecoverySuccessCount)
	require.Equal(t, int64(2000), snapshot.responsesBootstrapRecoveryWaitMs)
}

func TestAddCountersRestoresBootstrapRecoveryFields(t *testing.T) {
	bucket := &atomicBucket{}

	bucket.addCounters(counters{
		requestCount:                           2,
		successCount:                           1,
		responsesBootstrapRecoveryCount:        3,
		responsesBootstrapRecoverySuccessCount: 2,
		responsesBootstrapRecoveryWaitMs:       900,
	})

	snapshot := bucket.snapshot()
	require.Equal(t, int64(2), snapshot.requestCount)
	require.Equal(t, int64(1), snapshot.successCount)
	require.Equal(t, int64(3), snapshot.responsesBootstrapRecoveryCount)
	require.Equal(t, int64(2), snapshot.responsesBootstrapRecoverySuccessCount)
	require.Equal(t, int64(900), snapshot.responsesBootstrapRecoveryWaitMs)
}

func TestBuildQueryResultIncludesBootstrapOnlyBuckets(t *testing.T) {
	bucketTs := bucketStart(time.Now().Unix())
	result := buildQueryResult("gpt-test", map[bucketKey]counters{
		{model: "gpt-test", group: "default", bucketTs: bucketTs}: {
			responsesBootstrapRecoveryCount:        2,
			responsesBootstrapRecoverySuccessCount: 1,
			responsesBootstrapRecoveryWaitMs:       3000,
		},
	})

	require.Len(t, result.Groups, 1)
	group := result.Groups[0]
	require.Equal(t, int64(0), group.RequestCount)
	require.Equal(t, int64(2), group.ResponsesBootstrapRecoveryCount)
	require.Equal(t, int64(1), group.ResponsesBootstrapRecoverySuccessCount)
	require.Equal(t, int64(1500), group.AvgResponsesBootstrapRecoveryWaitMs)
	require.Equal(t, float64(50), group.ResponsesBootstrapRecoverySuccessRate)
	require.Len(t, group.Series, 1)
	require.Equal(t, int64(2), group.Series[0].ResponsesBootstrapRecoveryCount)
}

func syncMapForTest(t *testing.T) *sync.Map {
	t.Helper()
	original := hotBuckets
	t.Cleanup(func() {
		hotBuckets = original
	})
	return &sync.Map{}
}
