package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiChatToResponsesStreamHandlerRejectsMalformedNativeCustomToolInput(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	for _, custom := range []map[string]any{
		{"name": "apply_patch"},
		{"name": "apply_patch", "input": map[string]any{"patch": "*** Begin Patch"}},
	} {
		toolIndex := 0
		toolChunk := dto.ChatCompletionsStreamResponse{
			Id:      "chatcmpl_custom_invalid",
			Object:  "chat.completion.chunk",
			Created: 1710000001,
			Model:   "gpt-5",
			Choices: []dto.ChatCompletionsStreamResponseChoice{{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					ToolCalls: []dto.ToolCallResponse{{
						Index:  common.GetPointer(toolIndex),
						ID:     "call_custom_1",
						Type:   dto.CustomType,
						Custom: mustMarshalJSON(t, custom),
					}},
				},
			}},
		}
		finishReason := "tool_calls"
		finishChunk := dto.ChatCompletionsStreamResponse{
			Id:      "chatcmpl_custom_invalid",
			Object:  "chat.completion.chunk",
			Created: 1710000001,
			Model:   "gpt-5",
			Choices: []dto.ChatCompletionsStreamResponseChoice{{
				Index:        0,
				FinishReason: &finishReason,
			}},
		}

		body := strings.Builder{}
		body.WriteString(marshalStreamChunk(t, toolChunk))
		body.WriteString(marshalStreamChunk(t, finishChunk))
		body.WriteString("data: [DONE]\n")

		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(body.String()))}
		info := &relaycommon.RelayInfo{
			DisablePing: true,
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: "gpt-5",
			},
		}

		_, apiErr := OaiChatToResponsesStreamHandlerWithOptions(
			c,
			info,
			resp,
			service.ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true},
		)
		require.NotNil(t, apiErr)
		require.Contains(t, apiErr.Error(), "custom tool input")
	}
}
