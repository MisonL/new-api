package coze

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertCozeChatRequestPreservesUserAndAssistantHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("bot_id", "bot")
	converted, err := convertCozeChatRequest(c, dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "user", Content: "Question"},
		{Role: "assistant", Content: "Answer"},
	}})
	require.NoError(t, err)
	require.Equal(t, []CozeEnterMessage{
		{Role: "user", Content: "Question", ContentType: "text"},
		{Role: "assistant", Content: "Answer", ContentType: "text"},
	}, converted.AdditionalMessages)
}

func TestConvertCozeChatRequestRejectsSystemAndMediaContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err := convertCozeChatRequest(c, dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "system",
		Content: "Policy",
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role")

	_, err = convertCozeChatRequest(c, dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "user",
		Content: common.RawMessage(`[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]`),
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "image_url")
}
