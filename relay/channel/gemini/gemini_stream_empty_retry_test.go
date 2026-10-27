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

// Gemini 上游同样会在首个内容帧之前断流（GOAWAY / EOF）。旧行为是直接
// return usage, nil，客户端拿到空回复还按成功结算；这里要求与 OpenAI/Claude
// 一致：尚未提交内容时返回可重试错误。

func newGeminiStreamContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-pro:streamGenerateContent", nil)
	return c
}

func geminiStreamInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-pro"},
	}
}

func geminiFrameBody(t *testing.T, chunk dto.GeminiChatResponse) string {
	t.Helper()
	data, err := common.Marshal(chunk)
	require.NoError(t, err)
	return "data: " + string(data) + "\n\n"
}

func TestGeminiStreamInterruptedBeforeContentIsRetryable(t *testing.T) {
	c := newGeminiStreamContext(t)
	info := geminiStreamInfo()

	// 只有 usageMetadata、没有候选内容的分片：客户端什么都没收到。
	body := geminiFrameBody(t, dto.GeminiChatResponse{
		UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 120000},
	})
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader([]byte(body)))}

	usage, apiErr := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})

	require.Nil(t, usage)
	require.NotNil(t, apiErr, "只收到元数据分片就断流时必须报错，而不是返回空回复")
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
}

func TestGeminiStreamInterruptedAfterContentIsNotRetryable(t *testing.T) {
	c := newGeminiStreamContext(t)
	info := geminiStreamInfo()

	body := geminiFrameBody(t, dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{
				Role:  "model",
				Parts: []dto.GeminiPart{{Text: "hi"}},
			},
		}},
	})
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader([]byte(body)))}

	usage, apiErr := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})

	require.Nil(t, apiErr, "内容已下发后断流保持原行为，不能重放")
	require.NotNil(t, usage)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe),
		"内容帧到达后必须清掉 prelude 重试标记")
}

func TestGeminiStreamHealthyWithFinishReasonSucceeds(t *testing.T) {
	c := newGeminiStreamContext(t)
	info := geminiStreamInfo()

	finishReason := "STOP"
	body := geminiFrameBody(t, dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{
				Role:  "model",
				Parts: []dto.GeminiPart{{Text: "hi"}},
			},
			FinishReason: &finishReason,
		}},
	})
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader([]byte(body)))}

	_, apiErr := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
		return true
	})

	require.Nil(t, apiErr)
}
