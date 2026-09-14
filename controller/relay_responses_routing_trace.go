package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func shouldTrackResponsesRouting(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	return info.RelayMode == relayconstant.RelayModeResponses ||
		info.RelayMode == relayconstant.RelayModeResponsesCompact
}

func responsesRoutingTrace(c *gin.Context) *dto.ResponsesRoutingTrace {
	if c == nil {
		return nil
	}
	if trace, ok := common.GetContextKeyType[*dto.ResponsesRoutingTrace](c, constant.ContextKeyResponsesRoutingTrace); ok && trace != nil {
		return trace
	}
	trace := &dto.ResponsesRoutingTrace{}
	common.SetContextKey(c, constant.ContextKeyResponsesRoutingTrace, trace)
	return trace
}

func recordResponsesRoutingCandidate(c *gin.Context, channelID int) {
	if channelID <= 0 {
		return
	}
	trace := responsesRoutingTrace(c)
	if trace == nil {
		return
	}
	for _, existing := range trace.Candidates {
		if existing == channelID {
			return
		}
	}
	trace.Candidates = append(trace.Candidates, channelID)
	if diagnostics, ok := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](c, constant.ContextKeyChannelRouteDiagnostics); ok {
		trace.CandidateDetails = append(trace.CandidateDetails, dto.ResponsesRoutingCandidate{
			ChannelID: channelID,
			Route:     responsesRoutingRouteFromDiagnostics(diagnostics),
		})
	}
}

func recordResponsesRoutingPreflightSkip(c *gin.Context, skip dto.ResponsesRoutingChannelSkip) {
	if c == nil || skip.ChannelID <= 0 || strings.TrimSpace(skip.Reason) == "" {
		return
	}
	trace := responsesRoutingTrace(c)
	if trace == nil {
		return
	}
	for _, existing := range trace.PreflightSkipped {
		if existing == skip {
			return
		}
	}
	trace.PreflightSkipped = append(trace.PreflightSkipped, skip)
}

func beginResponsesRoutingAttempt(c *gin.Context, channelID int) int {
	trace := responsesRoutingTrace(c)
	if trace == nil || channelID <= 0 {
		return -1
	}
	trace.AttemptedChannels = append(trace.AttemptedChannels, dto.ResponsesRoutingAttempt{
		ChannelID: channelID,
	})
	if diagnostics, ok := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](c, constant.ContextKeyChannelRouteDiagnostics); ok {
		trace.AttemptedChannels[len(trace.AttemptedChannels)-1].Route = responsesRoutingRouteFromDiagnostics(diagnostics)
	}
	return len(trace.AttemptedChannels) - 1
}

func responsesRoutingRouteFromDiagnostics(diagnostics dto.ChannelRouteSelectionDiagnostics) dto.ResponsesRoutingRoute {
	return dto.ResponsesRoutingRoute{
		Group:               diagnostics.Group,
		RequestedModel:      diagnostics.RequestedModel,
		EffectiveRouteModel: diagnostics.EffectiveRouteModel,
		RouteScope:          diagnostics.RouteScope,
		RoutePriority:       diagnostics.RoutePriority,
		RouteWeightBPS:      diagnostics.RouteWeightBPS,
		WeightSource:        diagnostics.WeightSource,
		SelectionPath:       diagnostics.SelectionPath,
	}
}

func completeResponsesRoutingAttempt(c *gin.Context, index int, err *types.NewAPIError) {
	trace := responsesRoutingTrace(c)
	if trace == nil || index < 0 || index >= len(trace.AttemptedChannels) {
		return
	}
	attempt := &trace.AttemptedChannels[index]
	if err == nil {
		attempt.Outcome = "success"
		return
	}
	attempt.Outcome = "error"
	attempt.StatusCode = err.StatusCode
	attempt.ErrorCode = string(err.GetErrorCode())
	attempt.ErrorCategory = classifyRelayErrorCategory(c, nil, err)
}

func finalizeResponsesRoutingTrace(c *gin.Context, err *types.NewAPIError) {
	if c == nil {
		return
	}
	trace, ok := common.GetContextKeyType[*dto.ResponsesRoutingTrace](c, constant.ContextKeyResponsesRoutingTrace)
	if !ok || trace == nil {
		return
	}
	if len(trace.AttemptedChannels) > 0 {
		last := trace.AttemptedChannels[len(trace.AttemptedChannels)-1]
		trace.FinalChannel = last.ChannelID
		for index := len(trace.AttemptedChannels) - 1; index >= 0; index-- {
			if trace.AttemptedChannels[index].Outcome == "success" {
				trace.FinalChannel = trace.AttemptedChannels[index].ChannelID
				break
			}
		}
	}
	if err == nil {
		trace.TerminalReason = "success"
		return
	}
	trace.TerminalReason = routingTerminalReason(err)
}

func responsesRoutingAttributedChannelID(c *gin.Context, fallback int) int {
	trace := responsesRoutingTrace(c)
	if trace == nil {
		return fallback
	}
	if trace.FinalChannel > 0 {
		return trace.FinalChannel
	}
	for index := len(trace.AttemptedChannels) - 1; index >= 0; index-- {
		if trace.AttemptedChannels[index].ChannelID > 0 {
			return trace.AttemptedChannels[index].ChannelID
		}
	}
	return fallback
}

func routingTerminalReason(err *types.NewAPIError) string {
	if err == nil {
		return "success"
	}
	if err.StatusCode == http.StatusTooManyRequests {
		return "rate_limited"
	}
	if err.StatusCode == http.StatusUnauthorized || err.StatusCode == http.StatusForbidden {
		return "authentication_failed"
	}
	if types.IsChannelError(err) {
		return "channel_unavailable"
	}
	return "error"
}
