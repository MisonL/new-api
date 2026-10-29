package gemini

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

// 模型列表是最小的 JSON 信封，读取必须封顶：上游返回超大响应时，
// 每次抓取都整段进内存。这里用分块写入模拟超大响应，断言返回错误
// 而不是把整个响应体读进来。
func TestFetchGeminiModelsRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	chunk := bytes.Repeat([]byte("a"), 64<<10)
	chunks := int(common.UpstreamSmallResponseLimit/(64<<10)) + 2

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < chunks; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	service.InitHttpClient()
	_, err := FetchGeminiModels(context.Background(), server.URL, "test-key", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}
