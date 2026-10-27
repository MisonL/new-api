package service

import (
	"testing"
	"time"
)

// legacyTaskCutoff 保护的是"该功能上线前旧系统遗留的任务"：超时后不自动
// 退款，交给管理员处理。常量值必须与注释声明的上线日一致。
// 修复前注释写 2026-02-22，值 1740182400 却是 2025-02-22，导致多出整整
// 一年的遗留任务被当作新任务自动退款。
func TestLegacyTaskCutoffMatchesDocumentedDate(t *testing.T) {
	want := time.Date(2026, 2, 22, 0, 0, 0, 0, time.UTC).Unix()
	if legacyTaskCutoff != want {
		t.Fatalf("legacyTaskCutoff=%d (%s)，应为 %d (%s)",
			legacyTaskCutoff, time.Unix(legacyTaskCutoff, 0).UTC().Format(time.RFC3339),
			want, time.Unix(want, 0).UTC().Format(time.RFC3339))
	}
}
