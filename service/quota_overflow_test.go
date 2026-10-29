package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// 越界换算得到 MinInt64 时，结算路径会把负额度当退款（IncreaseUserQuota），
// 即"向用户加钱"。换算必须饱和到上限：金额依然荒谬，但方向是补扣而不是退款。
func TestCalculateAudioQuotaSaturatesInsteadOfGoingNegative(t *testing.T) {
	info := QuotaInfo{
		UsePrice:   true,
		ModelPrice: 1.18e21,
		GroupRatio: 1,
	}

	quota := calculateAudioQuota(info)

	require.Positive(t, quota, "an overflowing price must not produce a negative (refund) quota")
	require.Equal(t, math.MaxInt64, quota)
}

func TestCalculateAudioQuotaTokenPathSaturates(t *testing.T) {
	info := QuotaInfo{
		ModelRatio: 1.18e21,
		GroupRatio: 1,
		InputDetails: TokenDetails{
			TextTokens: 1000,
		},
	}

	quota := calculateAudioQuota(info)

	require.Positive(t, quota, "an overflowing ratio must not produce a negative (refund) quota")
	require.Equal(t, math.MaxInt64, quota)
}
