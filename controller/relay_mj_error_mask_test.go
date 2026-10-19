package controller

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Midjourney 错误响应的 description/result 来自上游，可能带签名 URL 或凭据。
// 返回给调用方前必须经过 MaskSensitiveInfo，且不再用 log.Println 裸打印
// 整个 mjErr（Properties 里可能有带签名的图片 URL）。
func TestMidjourneyErrorResponseIsMasked(t *testing.T) {
	src, err := os.ReadFile("relay.go")
	if err != nil {
		t.Fatalf("read relay.go: %v", err)
	}
	if strings.Contains(string(src), "log.Println(mjErr)") {
		t.Fatalf("midjourney error must not be logged raw with log.Println")
	}
	masked := regexp.MustCompile(`"description":\s*common\.MaskSensitiveInfo\(fmt\.Sprintf\("%s %s", mjErr\.Description, mjErr\.Result\)\)`)
	if !masked.Match(src) {
		t.Fatalf("midjourney error description must be masked before returning it")
	}
}
