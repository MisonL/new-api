package pricingrepair

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCalculateQuotaUsesUpdatedRatios(t *testing.T) {
	quota := CalculateQuota(LogSnapshot{
		PromptTokens:     50000,
		CompletionTokens: 38,
		GroupRatio:       1,
	}, ModelPricing{
		ModelRatio:      0.3,
		CompletionRatio: 11.0 / 3.0,
		CacheRatio:      0.5,
	})

	require.Equal(t, int64(15042), quota)
}

func TestCalculateQuotaSupportsCacheTokens(t *testing.T) {
	quota := CalculateQuota(LogSnapshot{
		PromptTokens:     1000,
		CompletionTokens: 100,
		CacheTokens:      400,
		GroupRatio:       1.2,
	}, ModelPricing{
		ModelRatio:      0.28,
		CompletionRatio: 3,
		CacheRatio:      0.5,
	})

	require.Equal(t, int64(370), quota)
}

func TestCalculateQuotaForZeroUsageReturnsZero(t *testing.T) {
	quota := CalculateQuota(LogSnapshot{}, ModelPricing{
		ModelRatio:      0.28,
		CompletionRatio: 3,
		CacheRatio:      0.5,
	})

	require.Zero(t, quota)
}

func TestHourBucketRoundsDownToHour(t *testing.T) {
	require.Equal(t, int64(7200), HourBucket(7359))
	require.Equal(t, int64(3600), HourBucket(3601))
}
