package perfmetrics

import (
	"sync"
	"testing"
	"time"

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
		Model:     "gpt-test",
		Group:     "default",
		LatencyMs: 100,
		Success:   true,
	})

	hotBuckets.Store(bucketKey{model: "gpt-test", group: "legacy", bucketTs: bucketTs}, legacyBucket)
	hotBuckets.Store(bucketKey{model: "gpt-test", group: "default", bucketTs: bucketTs}, defaultBucket)

	totals := map[string]counters{}
	mergeHotBucketSummaries(totals, bucketTs-1, bucketTs+1, allowedGroupSet([]string{"default"}))

	require.Len(t, totals, 1)
	require.Equal(t, int64(1), totals["gpt-test"].requestCount)
	require.Equal(t, int64(100), totals["gpt-test"].totalLatencyMs)
}

func syncMapForTest(t *testing.T) sync.Map {
	t.Helper()
	original := hotBuckets
	t.Cleanup(func() {
		hotBuckets = original
	})
	return sync.Map{}
}
