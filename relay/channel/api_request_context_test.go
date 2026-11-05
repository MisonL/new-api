package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// DoApiRequest/DoFormRequest/DoTaskApiRequest 此前用 http.NewRequest 构造上游
// 请求：这不是"没有超时"的细节，而是请求不携带任何 context。客户端断开、
// 上游取消、RELAY_TIMEOUT 都无法传到 net/http，挂死的上游会把连接、计费会话
// 与并发租约一起长期占住。必须用 NewRequestWithContext 绑定客户端 context。

type contextProbeAdaptor struct {
	seen chan context.Context
	url  string
}

func (a *contextProbeAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *contextProbeAdaptor) GetRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (a *contextProbeAdaptor) SetupRequestHeader(_ *gin.Context, _ *http.Header, _ *relaycommon.RelayInfo) error {
	return nil
}

func (a *contextProbeAdaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.GeneralOpenAIRequest) (any, error) {
	return nil, nil
}

func (a *contextProbeAdaptor) ConvertRerankRequest(_ *gin.Context, _ int, _ dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *contextProbeAdaptor) ConvertEmbeddingRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.EmbeddingRequest) (any, error) {
	return nil, nil
}

func (a *contextProbeAdaptor) ConvertAudioRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.AudioRequest) (io.Reader, error) {
	return nil, nil
}

func (a *contextProbeAdaptor) ConvertImageRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.ImageRequest) (any, error) {
	return nil, nil
}

func (a *contextProbeAdaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.OpenAIResponsesRequest) (any, error) {
	return nil, nil
}

func (a *contextProbeAdaptor) DoRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (any, error) {
	return nil, nil
}

func (a *contextProbeAdaptor) DoResponse(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	return nil, nil
}

func (a *contextProbeAdaptor) ConvertClaudeRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.ClaudeRequest) (any, error) {
	return nil, nil
}

func (a *contextProbeAdaptor) ConvertGeminiRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.GeminiChatRequest) (any, error) {
	return nil, nil
}

func (a *contextProbeAdaptor) GetModelList() []string { return nil }
func (a *contextProbeAdaptor) GetChannelName() string { return "context-probe" }

func newContextProbeServer(t *testing.T, seen chan<- context.Context) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDoApiRequestBindsClientContext(t *testing.T) {
	service.InitHttpClient()
	seen := make(chan context.Context, 1)
	server := newContextProbeServer(t, seen)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestCtx, cancel := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestCtx)
	cancel()

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	_, _ = DoApiRequest(&contextProbeAdaptor{url: server.URL}, ctx, info, nil)

	select {
	case got := <-seen:
		require.Error(t, got.Err(), "上游请求必须继承客户端 context，取消后应当已结束")
	case <-time.After(2 * time.Second):
		// 请求在到达服务端之前就被 context 取消拦下，同样是正确结果。
	}
}

// 若请求没有绑定 context，取消客户端不会影响上游：这里用一个阻塞的处理器
// 直接验证 DoApiRequest 会随客户端取消返回，而不是一直等到上游响应。
func TestDoApiRequestReturnsWhenClientCancels(t *testing.T) {
	service.InitHttpClient()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestCtx, cancel := context.WithCancel(context.Background())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestCtx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
		_, _ = DoApiRequest(&contextProbeAdaptor{url: server.URL}, ctx, info, nil)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("客户端取消后 DoApiRequest 仍未返回：上游请求没有绑定 context")
	}
}
