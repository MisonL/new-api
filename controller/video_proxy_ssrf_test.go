package controller

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

// doVideoProxyRequest 的每一次尝试都用调用方的请求上下文派生 attemptContext，
// 再用 req.Clone(attemptContext) 发出。Clone 只复制请求头与 body，不复制
// context，所以上游在 req 上打的 SSRF 标记会被整个替换掉。
//
// 共享 transport 的第一道门正是这个标记（service/http_client.go:119），标记
// 丢失后回环、内网与云元数据地址全部可达，并且 video_proxy 会把上游响应原样
// 回写给客户端。
func TestDoVideoProxyRequestPreservesSSRFMarker(t *testing.T) {
	recorder := &ssrfMarkerRecorder{}
	client := &http.Client{Transport: recorder}

	outerCtx := context.Background()
	req, err := http.NewRequestWithContext(outerCtx, http.MethodGet, "http://127.0.0.1:8080/internal", nil)
	require.NoError(t, err)
	req = req.WithContext(common.WithSSRFValidation(req.Context()))

	resp, err := doVideoProxyRequest(outerCtx, client, req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, []bool{true}, recorder.seen,
		"the SSRF marker must survive every proxy attempt context")
}

// 未打标记的请求（例如中继路径）不应被自动加上标记：这台 transport 只对
// 显式选择加入的 URL 校验目的地。
func TestDoVideoProxyRequestLeavesUnmarkedRequestUnmarked(t *testing.T) {
	recorder := &ssrfMarkerRecorder{}
	client := &http.Client{Transport: recorder}

	outerCtx := context.Background()
	req, err := http.NewRequestWithContext(outerCtx, http.MethodGet, "https://upstream.example/video.mp4", nil)
	require.NoError(t, err)

	resp, err := doVideoProxyRequest(outerCtx, client, req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, []bool{false}, recorder.seen)
}

// 真实 transport 的端到端证据：标记保留后，回环地址在拨号前就被拒绝，
// 本地监听端一次都不会被命中。
func TestVideoProxyRequestToLoopbackIsBlockedBySSRFGuard(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previous := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previous })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection: true,
		AllowPrivateIp:       false,
		AllowedPorts:         []string{"80", "443", "8080", "8443"},
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	var hits atomic.Int64
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			hits.Add(1)
			_ = conn.Close()
		}
	}()

	target := "http://" + listener.Addr().String() + "/internal"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	require.NoError(t, err)
	req = req.WithContext(common.WithSSRFValidation(req.Context()))

	client := service.GetHttpClient()
	if client == nil {
		// 包级 httpClient 由 main.go 初始化；测试里需要显式建一次。
		service.InitHttpClient()
		client = service.GetHttpClient()
	}
	require.NotNil(t, client)

	_, err = doVideoProxyRequest(context.Background(), client, req)
	// 主判据是"监听端一次都没被命中"：标记丢失时请求真的会连上去，
	// 此时即使因为对端不响应而报错，也已经构成了内网可达。
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int64(0), hits.Load(), "loopback listener must never be reached")
	require.Error(t, err, "the shared transport must refuse a loopback destination")
}

type ssrfMarkerRecorder struct {
	seen []bool
}

func (r *ssrfMarkerRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.seen = append(r.seen, common.RequiresSSRFValidation(req.Context()))
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     http.Header{},
	}, nil
}
