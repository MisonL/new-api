package service

import (
	"os"
	"regexp"
	"testing"
)

// 用户可控的文件/图片 URL（带签名参数）会经 DoDownloadRequest 下载。
// 直接下载分支已脱敏，worker 分支曾经裸打印，这里约束两条分支一致。
func TestDownloadLogsMaskOriginURLInBothBranches(t *testing.T) {
	src, err := os.ReadFile("download.go")
	if err != nil {
		t.Fatalf("read download.go: %v", err)
	}
	raw := regexp.MustCompile(`downloading file from worker: %s, reason: %s",\s*originUrl,`)
	if raw.Match(src) {
		t.Fatalf("worker download log must mask originUrl")
	}
	masked := regexp.MustCompile(`downloading file from worker: %s, reason: %s",\s*common\.MaskSensitiveInfo\(originUrl\),`)
	if !masked.Match(src) {
		t.Fatalf("worker download log must print common.MaskSensitiveInfo(originUrl)")
	}
}
