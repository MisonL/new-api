package cloudflare

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// bufio.NewScanner 默认 token 上限 64KiB。共享扫描器（stream_scanner.go:218）
// 显式把上限抬到 64MiB 并支持 StreamScannerMaxBufferMB 覆盖，本渠道的裸
// NewScanner 没有调 Buffer：同一帧超过 64KiB 时 Scan 停在 ErrTooLong，
// 流被截断，其后的 [DONE] 永远看不到，健康流被判成中断。
// 长帧与短帧只是尺寸差异，处理必须一致。
func TestCloudflareStreamCarriesFrameOver64KiB(t *testing.T) {
	long := strings.Repeat("b", 70*1024)
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"" + long + "\"}}]}\n\n" +
		"data: [DONE]\n\n"
	info, recorder := runCloudflareStream(t, body)

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"超过 64KiB 的帧不得截断流，[DONE] 必须仍然可达，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"超长帧后的 [DONE] 必须把流记为成功，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Contains(t, recorder.Body.String(), long,
		"超长帧的内容必须完整转发给客户端")
}
