package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

// 上游接受连接后一直不返回响应头时，RELAY_TIMEOUT=0（默认）的 client 没有
// 任何期限，流式扫描器的超时又只在收到响应后才启动。共享 Transport 必须
// 设置有限的 ResponseHeaderTimeout，否则每个卡住的上游都会长期占用一个
// 连接和 goroutine，直到客户端自己断开。
func TestRelayTransportSetsResponseHeaderTimeout(t *testing.T) {
	previous := constant.RelayResponseHeaderTimeout
	t.Cleanup(func() { constant.RelayResponseHeaderTimeout = previous })

	constant.RelayResponseHeaderTimeout = 300
	transport := newRelayTransport()
	require.Equal(t, 300*time.Second, transport.ResponseHeaderTimeout,
		"共享中继 Transport 必须限制等待响应头的时间")

	constant.RelayResponseHeaderTimeout = 0
	transport = newRelayTransport()
	require.Zero(t, transport.ResponseHeaderTimeout,
		"显式设为 0 时保持旧行为（不设响应头超时）")
}
