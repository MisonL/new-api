package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecordResponsesRoutingChannelSkipRetainsDistinctPreflightDecisions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	channelTwo := &model.Channel{Id: 2}
	channelOther := &model.Channel{Id: 168}

	recordResponsesRoutingChannelSkip(ctx, channelTwo, responsesRoutingChannelSkipReasonUnsupportedCustomTools)
	recordResponsesRoutingChannelSkip(ctx, channelTwo, responsesRoutingChannelSkipReasonUnsupportedCustomTools)
	recordResponsesRoutingChannelSkip(ctx, channelOther, responsesRoutingChannelSkipReasonUnsupportedNamespaceTools)

	skips, ok := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](ctx, constant.ContextKeyResponsesRoutingChannelSkips)
	require.True(t, ok)
	require.Equal(t, []dto.ResponsesRoutingChannelSkip{
		{ChannelID: 2, Reason: responsesRoutingChannelSkipReasonUnsupportedCustomTools},
		{ChannelID: 168, Reason: responsesRoutingChannelSkipReasonUnsupportedNamespaceTools},
	}, skips)
}

func TestSkipExplicitlyUnsupportedResponsesToolsRecordsAllReasons(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{}
	channel := &model.Channel{Id: 206}

	require.True(t, skipExplicitlyUnsupportedResponsesTools(ctx, info, channel, responsesUnsupportedToolCapabilities{
		function:   true,
		custom:     true,
		namespace:  true,
		toolSearch: true,
	}))
	require.ErrorIs(t, info.LastError, errResponsesFunctionToolsUnsupported)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{
		{ChannelID: channel.Id, Reason: responsesRoutingChannelSkipReasonUnsupportedFunctionTools},
		{ChannelID: channel.Id, Reason: responsesRoutingChannelSkipReasonUnsupportedCustomTools},
		{ChannelID: channel.Id, Reason: responsesRoutingChannelSkipReasonUnsupportedNamespaceTools},
		{ChannelID: channel.Id, Reason: responsesRoutingChannelSkipReasonUnsupportedToolSearch},
	})
}

func TestFinalizeResponsesRoutingTraceAttributesSuccessfulFinalChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	recordResponsesRoutingCandidate(ctx, 117)
	recordResponsesRoutingCandidate(ctx, 205)
	recordResponsesRoutingPreflightSkip(ctx, dto.ResponsesRoutingChannelSkip{
		ChannelID: 117,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedNamespaceTools,
	})
	first := beginResponsesRoutingAttempt(ctx, 117)
	completeResponsesRoutingAttempt(ctx, first, types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway))
	second := beginResponsesRoutingAttempt(ctx, 205)
	completeResponsesRoutingAttempt(ctx, second, nil)

	finalizeResponsesRoutingTrace(ctx, nil)
	trace, ok := common.GetContextKeyType[*dto.ResponsesRoutingTrace](ctx, constant.ContextKeyResponsesRoutingTrace)
	require.True(t, ok)
	require.Equal(t, 205, trace.FinalChannel)
	require.Equal(t, "success", trace.TerminalReason)
	require.Equal(t, 205, responsesRoutingAttributedChannelID(ctx, 117))
	require.Equal(t, []int{117, 205}, trace.Candidates)
	require.Equal(t, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: 117,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedNamespaceTools,
	}}, trace.PreflightSkipped)
	require.Equal(t, "error", trace.AttemptedChannels[0].Outcome)
	require.Equal(t, "success", trace.AttemptedChannels[1].Outcome)
}
