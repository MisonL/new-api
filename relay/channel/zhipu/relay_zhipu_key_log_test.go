package zhipu

import (
	"strings"
	"testing"
)

// 非法的 zhipu key 格式会走进错误日志。完整密钥（可能是用户误粘的其它厂商
// 有效密钥）不能原样落盘，日志里只保留长度与脱敏片段。
func TestInvalidZhipuKeyLogMessageMasksSecret(t *testing.T) {
	secret := "sk-live-0123456789abcdef.secret-part-uvwxyz"

	message := invalidZhipuKeyLogMessage(secret)

	if strings.Contains(message, secret) {
		t.Fatalf("log message leaks the raw key: %s", message)
	}
	if strings.Contains(message, "0123456789abcdef") || strings.Contains(message, "secret-part-uvwxyz") {
		t.Fatalf("log message leaks key material: %s", message)
	}
	if !strings.Contains(message, "invalid zhipu key") {
		t.Fatalf("log message must keep the diagnostic prefix: %s", message)
	}
}
