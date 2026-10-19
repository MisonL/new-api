package controller

import (
	"os"
	"regexp"
	"testing"
)

// 支付回调由匿名请求触发（body 上限 512KB），失败时会把 body 写进日志。
// 回调体里可能带支付平台生成的 customer 信息、卡摘要或其它可关联字段，
// 因此必须与其它日志一样脱敏并截断，而不是整段原样落盘。
func TestPaymentWebhookBodyLogsAreMaskedAndBounded(t *testing.T) {
	raw := regexp.MustCompile(`body=%q"[^\n]*,\s*string\((\w+)\)\)\)`)
	files := []string{"topup_creem.go", "topup_stripe.go", "topup_waffo_pancake.go"}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if raw.Match(src) {
			t.Fatalf("%s still logs the raw webhook body", file)
		}
	}
}
