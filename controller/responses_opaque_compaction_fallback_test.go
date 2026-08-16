package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesCompactRequestHasUnsafeCompactionInputDetectsOpaqueReplay(t *testing.T) {
	tests := []struct {
		name    string
		request dto.Request
	}{
		{
			name: "reasoning encrypted content",
			request: &dto.OpenAIResponsesCompactionRequest{
				Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
			},
		},
		{
			name: "future nested non-string encrypted content",
			request: &dto.OpenAIResponsesRequest{
				Input: common.RawMessage(`[{"type":"future_item","future_state":{"encrypted_content":{"version":2}}}]`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			info := opaqueCompactFallbackRelayInfo(constant.ChannelTypeOpenAI, dto.ResponsesCompactModeNative)
			info.Request = tt.request

			require.True(t, responsesCompactRequestHasUnsafeCompactionInput(c, info))
		})
	}
}

func TestOpaqueEncryptedReplayDisablesAllCompactionFallbacks(t *testing.T) {
	contextLengthErr := types.WithOpenAIError(types.OpenAIError{
		Message: "input too large for context window",
		Code:    "context_length_exceeded",
	}, http.StatusRequestEntityTooLarge)

	t.Run("native compact compatibility fallback", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		info := opaqueCompactFallbackRelayInfo(constant.ChannelTypeOpenAI, dto.ResponsesCompactModeAuto)
		err := types.WithOpenAIError(types.OpenAIError{
			Message: "Responses compact is unsupported by this upstream",
			Code:    string(types.ErrorCodeBadResponseStatusCode),
		}, http.StatusUnprocessableEntity)

		require.False(t, shouldFallbackResponsesCompactAuto(c, info, err))
	})

	t.Run("native context fallback after 413", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		info := opaqueCompactFallbackRelayInfo(constant.ChannelTypeOpenAI, dto.ResponsesCompactModeNative)

		require.False(t, shouldFallbackResponsesCompactNativeContext(c, info, contextLengthErr))
	})

	t.Run("visible-only fallback after 413", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		info := opaqueCompactFallbackRelayInfo(constant.ChannelTypeOpenAI, dto.ResponsesCompactModeSynthetic)

		require.False(t, shouldFallbackResponsesCompactVisibleOnly(c, info, contextLengthErr))
	})

	t.Run("summary model fallback after 413", func(t *testing.T) {
		for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeCodex} {
			c, _ := gin.CreateTestContext(nil)
			info := opaqueCompactFallbackRelayInfo(channelType, dto.ResponsesCompactModeSynthetic)
			info.UpstreamModelName = "gpt-5.6-sol"

			require.False(t, shouldFallbackResponsesCompactSummaryModel(c, info, contextLengthErr))
		}
	})

	t.Run("previous response visible-only recovery", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		info := opaqueCompactFallbackRelayInfo(constant.ChannelTypeOpenAI, dto.ResponsesCompactModeAuto)
		request := opaqueCompactFallbackRequest()
		request.PreviousResponseID = "resp_newapi_synthcmp_missing"
		info.Request = request
		err := types.NewErrorWithStatusCode(
			service.ErrResponsesRESTPreviousIDUnsupported,
			types.ErrorCodeConvertRequestFailed,
			http.StatusBadRequest,
		)

		require.False(t, shouldFallbackResponsesCompactPreviousResponseID(c, info, err))
	})

	t.Run("remote compaction synthetic recovery after 502", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request.Header.Set("X-Codex-Beta-Features", "remote_compaction_v2")
		info := opaqueCompactFallbackRelayInfo(constant.ChannelTypeOpenAI, dto.ResponsesCompactModeNative)
		info.RelayMode = relayconstant.RelayModeResponses
		info.IsStream = true
		info.Request = &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},
				{"type":"compaction_trigger"}
			]`),
		}
		err := types.NewErrorWithStatusCode(
			errors.New("bad response status code 502"),
			types.ErrorCodeBadResponseStatusCode,
			http.StatusBadGateway,
		)

		require.False(t, shouldFallbackRemoteResponsesCompaction(c, info, err))
	})
}

func opaqueCompactFallbackRelayInfo(channelType int, mode dto.ResponsesCompactMode) *relaycommon.RelayInfo {
	settings := dto.ChannelOtherSettings{ResponsesCompactMode: mode}
	return &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol",
		Request:         opaqueCompactFallbackRequest(),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:            206,
			ChannelType:          channelType,
			ChannelOtherSettings: settings,
			UpstreamModelName:    "gpt-5.6-sol",
		},
	}
}

func opaqueCompactFallbackRequest() *dto.OpenAIResponsesCompactionRequest {
	return &dto.OpenAIResponsesCompactionRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque-reasoning-state","future_state":{"version":2}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
}
