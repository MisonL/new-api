package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

// GetScannerBufferSize 是自建扫描器渠道的行上限入口：默认 64MiB，
// 且必须跟随 StreamScannerMaxBufferMB 覆盖，与共享扫描器同一判据。
// 没有这个导出入口，各渠道只能各写一个常量，环境变量就只对共享路径生效。
func TestGetScannerBufferSizeFollowsEnvOverride(t *testing.T) {
	previous := constant.StreamScannerMaxBufferMB
	t.Cleanup(func() { constant.StreamScannerMaxBufferMB = previous })

	constant.StreamScannerMaxBufferMB = 0
	require.Equal(t, DefaultMaxScannerBufferSize, GetScannerBufferSize(),
		"未设置环境变量时必须回落到默认 64MiB")

	constant.StreamScannerMaxBufferMB = 4
	require.Equal(t, 4<<20, GetScannerBufferSize(),
		"StreamScannerMaxBufferMB=4 时上限必须是 4MiB，与共享扫描器一致")
}
