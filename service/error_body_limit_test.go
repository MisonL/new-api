package service

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// countingRepeatBody 输出 size 字节 'a'，并记录实际被读取的字节数。
type countingRepeatBody struct {
	remaining int64
	read      int64
}

func (b *countingRepeatBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > b.remaining {
		n = b.remaining
	}
	for i := int64(0); i < n; i++ {
		p[i] = 'a'
	}
	b.remaining -= n
	b.read += n
	return int(n), nil
}

func (b *countingRepeatBody) Close() error { return nil }

// 上游错误页可能达到 MB 级（Ollama 8MB、Dify 1MB）。RelayErrorHandler 把
// resp.Body 全量 io.ReadAll：错误路径本身就是内存放大的入口，多个并发失败
// 请求各持有一份完整响应体。读取必须有上限。
func TestRelayErrorHandlerBoundsUpstreamErrorBodyRead(t *testing.T) {
	t.Parallel()

	body := &countingRepeatBody{remaining: 64 << 20}
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       body,
	}

	err := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Less(t, body.read, int64(4<<20),
		"an upstream error body must not be read in full (read %d bytes)", body.read)
}
