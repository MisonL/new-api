package controller

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// 共享客户端在 RELAY_TIMEOUT=0（默认）时没有 Timeout，transport 也没有
// ResponseHeaderTimeout。上游接受连接后不发响应头，请求会一直挂住；后台
// 巡检用运行标志做 CompareAndSwap，一旦卡住就再也不会被调度，单个挂起的
// 上游即可让全局模型巡检停摆。
func TestGetResponseBodyWithTimeoutGivesUpOnSilentUpstream(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	// 接受连接但永远不写响应。
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn // 故意不关闭、不写任何字节
		}
	}()

	baseURL := "http://" + listener.Addr().String()
	channel := &model.Channel{Id: 1, Type: 1, BaseURL: &baseURL}
	start := time.Now()
	_, err = GetResponseBodyWithTimeout(http.MethodGet, "http://"+listener.Addr().String()+"/v1/models",
		channel, http.Header{}, 300*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err, "a silent upstream must not hang the caller")
	require.Less(t, elapsed, 5*time.Second, "the timeout must actually bound the request")
}
