package common

import "testing"

// 重试次数默认值必须在全新安装上就允许 failover：这是"自动重试"功能的出厂契约。
// 0 会让 shouldRetry 在 retryTimes <= 0 处直接返回 false，状态码配置、渠道健康
// 观测、402/400 可重试等等全部空转——全新安装不手动调大这个值就没有任何重试。
// 生产环境使用 3（见 docs/reviews/CR-FULL-SCENARIO-AUDIT-2026-09-29.md），
// 默认值与之对齐。
func TestDefaultRetryTimesEnablesFailover(t *testing.T) {
	if RetryTimes < 1 {
		t.Fatalf("RetryTimes 默认值 = %d，全新安装将没有任何渠道 failover；植入重试功能的契约要求 >= 1", RetryTimes)
	}
}
