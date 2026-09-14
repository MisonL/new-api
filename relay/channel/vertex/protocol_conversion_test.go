package vertex

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIImagenRequestJoinsAllUserText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	a := &Adaptor{RequestMode: RequestModeGemini}
	converted, err := a.ConvertOpenAIRequest(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "imagen-3.0-generate-001"}}, &dto.GeneralOpenAIRequest{
		Model: "imagen-3.0-generate-001",
		Messages: []dto.Message{
			{Role: "user", Content: "first"},
			{Role: "user", Content: "second"},
		},
	})
	require.NoError(t, err)
	request, ok := converted.(dto.GeminiImageRequest)
	require.True(t, ok)
	require.Equal(t, "first\nsecond", request.Instances[0].Prompt)
}

func TestConvertOpenAIImagenRequestIgnoresNonUserHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	a := &Adaptor{RequestMode: RequestModeGemini}
	converted, err := a.ConvertOpenAIRequest(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "imagen-3.0-generate-001"}}, &dto.GeneralOpenAIRequest{
		Model: "imagen-3.0-generate-001",
		Messages: []dto.Message{
			{Role: "system", Content: "do not reject this history"},
			{Role: "assistant", Content: "previous response"},
			{Role: "user", Content: "draw a fox"},
		},
	})
	require.NoError(t, err)
	request, ok := converted.(dto.GeminiImageRequest)
	require.True(t, ok)
	require.Equal(t, "draw a fox", request.Instances[0].Prompt)
}

func TestConvertOpenAIImagenRequestRejectsNonTextContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	a := &Adaptor{RequestMode: RequestModeGemini}
	_, err := a.ConvertOpenAIRequest(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "imagen-3.0-generate-001"}}, &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role:    "user",
			Content: common.RawMessage(`[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]`),
		}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "image_url")
}
