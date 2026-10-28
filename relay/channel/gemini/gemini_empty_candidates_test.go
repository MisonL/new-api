package gemini

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Gemini 用 HTTP 200 携带 promptFeedback.blockReason 或空 candidates 表达失败。
// 旧实现在 handler 内自己 c.JSON 写出错误体，然后 return &usage, nil：调用方
// 视为成功，按估算 prompt token 扣费、不记渠道失败、不重试，客户端却收到错误。
// 与 OpenAI 通道对"200 响应体即错误"的处理一致，这里应返回 nil usage + 错误，
// 由上层统一写错误响应、退还预扣费并按状态码决定重试。
func geminiEmptyCandidateResponse(t *testing.T, payload dto.GeminiChatResponse) (*gin.Context, *http.Response) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body, err := common.Marshal(payload)
	require.NoError(t, err)
	return c, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
}

func geminiEmptyCandidateRelayInfo() *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gemini-3-flash-preview",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
	info.SetEstimatePromptTokens(120)
	return info
}

func TestGeminiChatHandlerBlockedPromptReturnsErrorWithoutUsage(t *testing.T) {
	blockReason := "SAFETY"
	c, resp := geminiEmptyCandidateResponse(t, dto.GeminiChatResponse{
		PromptFeedback: &dto.GeminiChatPromptFeedback{BlockReason: &blockReason},
		UsageMetadata:  dto.GeminiUsageMetadata{PromptTokenCount: 120},
	})

	usage, newAPIError := GeminiChatHandler(c, geminiEmptyCandidateRelayInfo(), resp)

	require.NotNil(t, newAPIError, "a blocked prompt must surface as an error")
	require.Equal(t, http.StatusBadRequest, newAPIError.StatusCode)
	require.Equal(t, types.ErrorCodePromptBlocked, newAPIError.GetErrorCode())
	require.Nil(t, usage, "a blocked prompt must not bill the estimated prompt tokens")
	require.False(t, c.Writer.Written(), "the handler must not write the error body itself")
	require.Equal(t, "gemini_block_reason=SAFETY",
		common.GetContextKeyString(c, constant.ContextKeyAdminRejectReason))
}

func TestGeminiChatHandlerEmptyCandidatesReturnsErrorWithoutUsage(t *testing.T) {
	c, resp := geminiEmptyCandidateResponse(t, dto.GeminiChatResponse{
		UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 120},
	})

	usage, newAPIError := GeminiChatHandler(c, geminiEmptyCandidateRelayInfo(), resp)

	require.NotNil(t, newAPIError, "empty candidates must surface as an error")
	require.Equal(t, http.StatusInternalServerError, newAPIError.StatusCode)
	require.Equal(t, types.ErrorCodeEmptyResponse, newAPIError.GetErrorCode())
	require.Nil(t, usage, "an empty response must not bill the estimated prompt tokens")
	require.False(t, c.Writer.Written(), "the handler must not write the error body itself")
	require.Equal(t, "gemini_empty_candidates",
		common.GetContextKeyString(c, constant.ContextKeyAdminRejectReason))
}
