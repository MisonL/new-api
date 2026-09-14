package dify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2DifyRejectsMalformedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	user, err := common.Marshal("test-user")
	require.NoError(t, err)

	_, err = requestOpenAI2Dify(c, &relaycommon.RelayInfo{}, dto.GeneralOpenAIRequest{
		User: user,
		Messages: []dto.Message{{
			Role:    "user",
			Content: common.RawMessage(`[{"type":"text"}`),
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "content")
}

func TestRequestOpenAI2DifyConvertsRemoteImageWithoutPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	user, err := common.Marshal("test-user")
	require.NoError(t, err)

	converted, err := requestOpenAI2Dify(c, &relaycommon.RelayInfo{}, dto.GeneralOpenAIRequest{
		User: user,
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": "https://example.com/image.png",
				},
			}},
		}},
	})

	require.NoError(t, err)
	require.Len(t, converted.Files, 1)
	require.Equal(t, "image", converted.Files[0].Type)
	require.Equal(t, "remote_url", converted.Files[0].TransferMode)
	require.Equal(t, "https://example.com/image.png", converted.Files[0].URL)
}

func TestRequestOpenAI2DifyRejectsUnsupportedAssistantMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	reasoning := "private"
	_, err := requestOpenAI2Dify(c, &relaycommon.RelayInfo{}, dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "assistant", Content: "history", ReasoningContent: &reasoning},
			{Role: "user", Content: "next"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reasoning_content")
}

func TestRequestOpenAI2DifyRejectsThinkingContentAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	reasoning := "private"
	_, err := requestOpenAI2Dify(c, &relaycommon.RelayInfo{}, dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "assistant", Content: "history", ThinkingContent: &reasoning},
			{Role: "user", Content: "next"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "reasoning_content")
}

func TestDifyHandlerReturnsUpstreamHTTP400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"code":"invalid_param","message":"bad query"}`)), Header: make(http.Header)}

	usage, apiErr := difyHandler(c, &relaycommon.RelayInfo{}, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "bad query")
}

func TestUploadDifyFileUsesChannelProxyConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://dify.example.test",
			ApiKey:         "dify-key",
			ChannelSetting: dto.ChannelSettings{Proxy: "ftp://invalid-proxy.example.test:21"},
		},
	}
	media := dto.MediaContent{
		Type:     dto.ContentTypeImageURL,
		ImageUrl: &dto.MessageImageUrl{Url: "data:image/png;base64,AA==", MimeType: "image/png"},
	}

	_, err := uploadDifyFile(c, info, "test-user", media)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Dify upload HTTP client")
	require.Contains(t, err.Error(), "unsupported proxy scheme")
}

func TestReadDifyResponseBodyRejectsOversizedPayload(t *testing.T) {
	_, err := readDifyResponseBody(strings.NewReader(strings.Repeat("x", 4)), 3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds 3 bytes")
}

func TestConvertClaudeRequestReturnsExplicitUnsupportedError(t *testing.T) {
	_, err := (&Adaptor{}).ConvertClaudeRequest(nil, nil, &dto.ClaudeRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}
