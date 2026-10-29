package common

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Go 的越界 float→int 转换在本机得到 MinInt64；结算路径把负额度当退款
// （IncreaseUserQuota），所以换算必须在越界时饱和而不是回绕成负数。
func TestSafeQuotaIntFromFloat64SaturatesOverflow(t *testing.T) {
	require.Equal(t, 42, SafeQuotaIntFromFloat64(42.0))
	require.Equal(t, 0, SafeQuotaIntFromFloat64(math.NaN()))
	require.Equal(t, math.MaxInt64, SafeQuotaIntFromFloat64(1.18e21))
	require.Equal(t, math.MaxInt64, SafeQuotaIntFromFloat64(math.Inf(1)))
	require.Equal(t, math.MinInt64, SafeQuotaIntFromFloat64(math.Inf(-1)))
}

func TestSafeQuotaIntFromDecimalSaturatesOverflow(t *testing.T) {
	require.Equal(t, 42, SafeQuotaIntFromDecimal(decimal.NewFromInt(42), true))

	// 1.18e21 的 IntPart 回绕成 -591620717411303424（负数），直接转 int 会把
	// 正向扣费变成退款。
	big, err := decimal.NewFromString("1.18e21")
	require.NoError(t, err)
	require.Equal(t, math.MaxInt64, SafeQuotaIntFromDecimal(big, true))

	negative, err := decimal.NewFromString("-1.18e21")
	require.NoError(t, err)
	require.Equal(t, math.MinInt64, SafeQuotaIntFromDecimal(negative, true))

	// roundFirst 语义保持：0.6 四舍五入为 1。
	require.Equal(t, 1, SafeQuotaIntFromDecimal(decimal.NewFromFloat(0.6), true))
	require.Equal(t, 0, SafeQuotaIntFromDecimal(decimal.NewFromFloat(0.6), false))
}
