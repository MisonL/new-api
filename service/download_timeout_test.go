package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func tryAllowLocalSSRFFixture(t *testing.T) {
	t.Helper()
	fetchSetting := system_setting.GetFetchSetting()
	previous := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previous })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		AllowedPorts:           []string{"1-65535"},
		ApplyIPFilterForDomain: true,
	}
	InitHttpClient()
}

// DoDownloadRequest 此前用 context.Background() 发起下载：没有截止时间，
// 上游挂住时连接与 goroutine 一直占用。这里用一个永不响应的服务端断言：
// 配置 RELAY_TIMEOUT 后，下载必须在超时后返回错误而不是一直挂起。
func TestDoDownloadRequestTimesOutOnHangingUpstream(t *testing.T) {
	tryAllowLocalSSRFFixture(t)

	previousTimeout := common.RelayTimeout
	common.RelayTimeout = 1
	t.Cleanup(func() { common.RelayTimeout = previousTimeout })

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	start := time.Now()
	_, err := DoDownloadRequest(server.URL)
	elapsed := time.Since(start)

	require.Error(t, err, "a hanging upstream must not block the download forever")
	require.Less(t, elapsed, 10*time.Second)
}

// 未配置 RELAY_TIMEOUT 时回退到有限上限，而不是退回无限等待。
func TestDownloadTimeoutFallsBackToBoundedDefault(t *testing.T) {
	previousTimeout := common.RelayTimeout
	t.Cleanup(func() { common.RelayTimeout = previousTimeout })

	common.RelayTimeout = 0
	require.Equal(t, defaultDownloadTimeout, downloadTimeout())

	common.RelayTimeout = 42
	require.Equal(t, 42*time.Second, downloadTimeout())
}

// cancel 必须挂在 body 的 Close 上：函数返回时就取消会掐断调用方的读取。
func TestCancelOnCloseBodyCancelsOnlyAfterClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	cancelled := make(chan struct{})
	body := &cancelOnCloseBody{ReadCloser: resp.Body, cancel: func() { close(cancelled) }}
	buf := make([]byte, 5)
	_, err = io.ReadFull(body, buf)
	require.NoError(t, err)
	require.Equal(t, "hello", string(buf))

	select {
	case <-cancelled:
		t.Fatal("cancel must not fire before the body is closed")
	default:
	}

	require.NoError(t, body.Close())
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("close must release the request context")
	}
}
