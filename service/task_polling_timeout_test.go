package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// stallingTaskAdaptor never produces an upstream response unless its context
// ends. It models a provider that accepts the connection and then stalls.
type stallingTaskAdaptor struct {
	mockAdaptor
}

func (a *stallingTaskAdaptor) FetchTaskWithContext(ctx context.Context, _ string, _ string, _ map[string]any, _ string, _ ...http.Header) (*http.Response, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// 任务轮询此前没有任何单请求超时：RELAY_TIMEOUT 默认为 0（客户端无 Timeout），
// 轮询上下文只有 WithCancel，没有 deadline。上游接受连接后挂起时，整个轮询
// 循环会永久停在这一条请求上，后续所有任务不再推进。
func TestFetchTaskWithContextBoundsStalledRequest(t *testing.T) {
	previous := taskPollingRequestTimeout
	taskPollingRequestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { taskPollingRequestTimeout = previous })

	start := time.Now()
	_, err := FetchTaskWithContext(context.Background(), &stallingTaskAdaptor{}, "http://127.0.0.1:1", "key", map[string]any{}, "")
	elapsed := time.Since(start)

	require.Error(t, err, "挂起的上游请求必须以超时结束，而不是永久阻塞轮询")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, elapsed, 5*time.Second, "超时必须由单请求超时控制，不能依赖上游自行结束")
}

// blockingBodyAdaptor returns headers immediately and stalls while the caller
// reads the body. The timeout must still cover the body read, otherwise a
// provider can send a response line and then hold the connection open forever.
type blockingBodyAdaptor struct {
	mockAdaptor
}

type blockingBody struct {
	ctx context.Context
}

func (b *blockingBody) Read(p []byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *blockingBody) Close() error { return nil }

func (a *blockingBodyAdaptor) FetchTaskWithContext(ctx context.Context, _ string, _ string, _ map[string]any, _ string, _ ...http.Header) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       &blockingBody{ctx: ctx},
	}, nil
}

func TestFetchTaskWithContextBoundsStalledBodyRead(t *testing.T) {
	previous := taskPollingRequestTimeout
	taskPollingRequestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { taskPollingRequestTimeout = previous })

	resp, err := FetchTaskWithContext(context.Background(), &blockingBodyAdaptor{}, "http://127.0.0.1:1", "key", map[string]any{}, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer CloseResponseBodyGracefully(resp)

	start := time.Now()
	_, readErr := io.ReadAll(resp.Body)
	require.Error(t, readErr, "读响应体同样受单请求超时约束")
	require.Less(t, time.Since(start), 5*time.Second)
}

// Closing the body must release the timeout resources and leave the response
// usable for normal callers.
func TestFetchTaskWithContextReturnsUsableResponse(t *testing.T) {
	succeeding := &succeedingTaskAdaptor{}
	resp, err := FetchTaskWithContext(context.Background(), succeeding, "http://127.0.0.1:1", "key", map[string]any{}, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer CloseResponseBodyGracefully(resp)

	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	require.Equal(t, "ok", string(body))
}

type succeedingTaskAdaptor struct {
	mockAdaptor
}

func (a *succeedingTaskAdaptor) FetchTaskWithContext(_ context.Context, _ string, _ string, _ map[string]any, _ string, _ ...http.Header) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
	}, nil
}

var _ TaskPollingAdaptor = (*stallingTaskAdaptor)(nil)
var _ TaskPollingAdaptor = (*blockingBodyAdaptor)(nil)
var _ TaskPollingAdaptor = (*succeedingTaskAdaptor)(nil)
var _ = model.Task{}
var _ = relaycommon.TaskInfo{}
