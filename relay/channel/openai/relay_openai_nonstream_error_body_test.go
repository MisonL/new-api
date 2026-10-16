package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 上游用 HTTP 200 + error 体报告失败（限流、配额等）时，非流式路径必须与
// 流式守卫 streamErrorFrameWithContent（relay-openai.go，Type/Message/Param/
// Code 任一非空）和 Responses 守卫 responsesOpenAIErrorHasContent 同口径。
// 原实现只认 Type != ""，code-only 与 message-only 的错误体被当成功处理：
// 返回 200、按估算 token 伪造 usage 结算，客户端拿到混入 usage 的错误体。
func callNonStreamWithErrorBody(t *testing.T, body string) (*httptest.ResponseRecorder, *dto.Usage, *types.NewAPIError) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "m"},
	}
	info.SetEstimatePromptTokens(11)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	usage, apiErr := OpenaiHandler(c, info, resp)
	return recorder, usage, apiErr
}

func TestOaiNonStreamFailsOnCodeOnlyErrorBody(t *testing.T) {
	_, _, apiErr := callNonStreamWithErrorBody(t, `{"error":{"code":"insufficient_quota"}}`)
	require.NotNil(t, apiErr, "code-only 错误体必须判失败，不能按成功结算")
}

func TestOaiNonStreamFailsOnMessageOnlyErrorBody(t *testing.T) {
	_, _, apiErr := callNonStreamWithErrorBody(t, `{"error":{"message":"upstream rate limited"}}`)
	require.NotNil(t, apiErr, "message-only 错误体必须判失败，不能按成功结算")
}

// 反向：空 error 对象与正常成功体不得误杀——与流式守卫的空占位帧规则一致。
func TestOaiNonStreamAcceptsEmptyErrorPlaceholder(t *testing.T) {
	_, _, apiErr := callNonStreamWithErrorBody(t,
		`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"error":{}}`)
	require.Nil(t, apiErr, "空 error 占位不是失败")
}
