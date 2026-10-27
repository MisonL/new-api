package relay

import "testing"

// Midjourney 任务行上的 Quota 被异步失败退款当作退款额（
// service.RefundMidjourneyTaskQuota）。只有真正扣过费的提交才允许留下金额：
// swap_face 仅在 200 + Code==1 时扣费，而它的可退款码集合包含 21/22，
// 未扣费却带金额的行在失败时会凭空给用户加额度。
func TestMidjourneyTaskRowQuotaIsZeroWhenSubmitDidNotCharge(t *testing.T) {
	const price = 25_000

	if got := midjourneyTaskQuotaForRow(true, price); got != price {
		t.Fatalf("已扣费的提交必须保留退款金额，得到 %d", got)
	}
	if got := midjourneyTaskQuotaForRow(false, price); got != 0 {
		t.Fatalf("未扣费的提交不得留下可退款金额，得到 %d", got)
	}
}
