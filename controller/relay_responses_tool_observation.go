package controller

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
)

func recordResponsesToolCapabilityObservation(c *gin.Context, info *relaycommon.RelayInfo) {
	if c == nil || info == nil || info.ChannelMeta == nil || info.RelayMode != relayconstant.RelayModeResponses {
		return
	}
	observation, ok := responsesToolCapabilityObservationFromContext(c, time.Now().UTC())
	if !ok {
		return
	}
	settings, err := model.MarkResponsesToolCapabilityObservation(info.ChannelMeta.ChannelId, observation)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("mark responses tool capability observation failed: %s", err.Error()))
		return
	}
	if settings.ResponsesCapabilityRegistry == nil {
		return
	}
	info.ChannelOtherSettings = settings
	info.ChannelMeta.ChannelOtherSettings = settings
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, settings)
	logResponsesToolCapabilityConflicts(c, info.ChannelMeta.ChannelId, settings, observation)
}

func responsesToolCapabilityObservationFromContext(c *gin.Context, observedAt time.Time) (dto.ResponsesToolCapabilityObservation, bool) {
	if c == nil {
		return dto.ResponsesToolCapabilityObservation{}, false
	}
	observation := dto.ResponsesToolCapabilityObservation{
		ObservedAt:        observedAt.UTC().Unix(),
		CustomToolCall:    common.GetContextKeyBool(c, constant.ContextKeyResponsesObservedCustomToolCall),
		NamespaceToolCall: common.GetContextKeyBool(c, constant.ContextKeyResponsesObservedNamespaceToolCall),
		ToolSearchCall:    common.GetContextKeyBool(c, constant.ContextKeyResponsesObservedToolSearchCall),
	}
	return observation, observation.HasEvidence()
}

func logResponsesToolCapabilityConflicts(c *gin.Context, channelID int, settings dto.ChannelOtherSettings, observation dto.ResponsesToolCapabilityObservation) {
	conflicts := make([]string, 0, 3)
	if observation.CustomToolCall && settings.ExplicitlyDisablesResponsesCustomTools() {
		conflicts = append(conflicts, "custom")
	}
	if observation.NamespaceToolCall && settings.ExplicitlyDisablesResponsesNamespaceTools() {
		conflicts = append(conflicts, "namespace")
	}
	if observation.ToolSearchCall && settings.ExplicitlyDisablesResponsesToolSearch() {
		conflicts = append(conflicts, "tool_search")
	}
	if len(conflicts) == 0 {
		return
	}
	logger.LogWarn(c, fmt.Sprintf(
		"responses tool capability observation conflicts with explicit channel settings: channel_id=%d capabilities=%v",
		channelID,
		conflicts,
	))
}
