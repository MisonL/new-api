package common

import (
	"math"

	"github.com/shopspring/decimal"
)

// Go 规范把越界 float→int 转换的结果定义为实现相关；本机实测（amd64/arm64）
// 得到 MinInt64。结算路径把负额度当作退款（IncreaseUserQuota），一个异常大的
// 倍率或价格换算就越界成"向用户加钱"。下面两个换算函数在越界时饱和到 int64
// 上限并告警：数值依然荒谬，但方向正确（补扣会因余额不足失败），不会静默变成
// 退款。

// SafeQuotaIntFromFloat64 把换算后的额度收敛到 int 可表示范围。NaN 视为 0。
func SafeQuotaIntFromFloat64(v float64) int {
	if math.IsNaN(v) {
		SysError("quota conversion received NaN, using 0")
		return 0
	}
	if v >= float64(math.MaxInt64) {
		SysError("quota conversion overflows int64, saturating to MaxInt64")
		return math.MaxInt64
	}
	if v <= float64(math.MinInt64) {
		SysError("quota conversion underflows int64, saturating to MinInt64")
		return math.MinInt64
	}
	return int(v)
}

// SafeQuotaIntFromDecimal 在调用 IntPart 之前先做范围比较：decimal.IntPart
// 对超出 int64 的值同样会回绕成负数。roundFirst 对应调用方原本的 Round(0)。
func SafeQuotaIntFromDecimal(d decimal.Decimal, roundFirst bool) int {
	if roundFirst {
		d = d.Round(0)
	}
	if d.GreaterThan(maxQuotaDecimal) {
		SysError("quota conversion overflows int64, saturating to MaxInt64")
		return math.MaxInt64
	}
	if d.LessThan(minQuotaDecimal) {
		SysError("quota conversion underflows int64, saturating to MinInt64")
		return math.MinInt64
	}
	return int(d.IntPart())
}

var (
	maxQuotaDecimal = decimal.NewFromInt(math.MaxInt64)
	minQuotaDecimal = decimal.NewFromInt(math.MinInt64)
)
