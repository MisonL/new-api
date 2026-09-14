package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoIncludesResponsesStreamObservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	observation := relaycommon.NewResponsesStreamObservation()
	observation.Observe(dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "partial"})
	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta:                &relaycommon.ChannelMeta{},
		IsStream:                   true,
		ReceivedResponseCount:      1,
		ResponsesStreamObservation: observation,
		StreamStatus:               relaycommon.NewStreamStatus(),
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, -1, -1)
	streamObservation, ok := other["responses_stream_observation"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, streamObservation["completion_unconfirmed"])
}

func TestGenerateTextOtherInfoOmitsResponsesStreamObservationWithoutResponsesEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	observation := relaycommon.NewResponsesStreamObservation()
	observation.MarkDone()
	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta:                &relaycommon.ChannelMeta{},
		IsStream:                   true,
		ReceivedResponseCount:      1,
		ResponsesStreamObservation: observation,
		StreamStatus:               relaycommon.NewStreamStatus(),
	}, 1, 1, 1, 0, 0, -1, -1)
	_, exists := other["responses_stream_observation"]
	require.False(t, exists)
}

func TestAppendResponsesRelayInfoIncludesResponsesStreamObservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	observation := relaycommon.NewResponsesStreamObservation()
	observation.Observe(dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "partial"})
	other := map[string]interface{}{}
	AppendResponsesRelayInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta:                &relaycommon.ChannelMeta{},
		ReceivedResponseCount:      1,
		ResponsesStreamObservation: observation,
	}, other)

	streamObservation, ok := other["responses_stream_observation"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, streamObservation["completion_unconfirmed"])
}
