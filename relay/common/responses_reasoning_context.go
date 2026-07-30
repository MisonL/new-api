package common

import (
	"bytes"
	"fmt"
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"

	"github.com/gin-gonic/gin"
)

// ApplyResponsesReasoningContextCapability applies the channel's explicit
// reasoning.context capability decision before a Responses request is sent.
// Unsupported channels lose only reasoning.context; supported and unknown
// channels retain the client value for native forwarding.
func ApplyResponsesReasoningContextCapability(c *gin.Context, info *RelayInfo, request dto.OpenAIResponsesRequest) dto.OpenAIResponsesRequest {
	if request.Reasoning == nil || len(bytes.TrimSpace(request.Reasoning.Context)) == 0 {
		return request
	}

	channelType := 0
	channelID := 0
	model := request.Model
	settings := dto.ChannelOtherSettings{}
	if info != nil {
		if info.OriginModelName != "" {
			model = info.OriginModelName
		}
		if info.ChannelMeta != nil {
			channelType = info.ChannelType
			channelID = info.ChannelId
			settings = info.ChannelOtherSettings
		}
	}

	capability := settings.ResolveResponsesReasoningContextCapability(channelType)
	action := "forwarded_unverified_capability"
	switch capability {
	case dto.ResponsesReasoningContextCapabilitySupported:
		action = "forwarded_supported_capability"
	case dto.ResponsesReasoningContextCapabilityUnsupported:
		strippedRequest, stripped := StripResponsesReasoningContext(request)
		if stripped {
			request = strippedRequest
			action = "stripped_unsupported_capability"
			basecommon.SysLog(fmt.Sprintf(
				"responses reasoning context stripped by channel capability: channel_id=%d model=%s profile=%s",
				channelID,
				model,
				settings.NormalizedResponsesUpstreamProfile(),
			))
		}
	}
	if c != nil {
		basecommon.SetContextKey(c, constant.ContextKeyResponsesReasoningContextAction, action)
	}
	return request
}

// ApplyResponsesLogicalModelReasoningEffort carries an effort suffix from the
// client-facing model through model mapping without changing the mapped model.
func ApplyResponsesLogicalModelReasoningEffort(info *RelayInfo, request dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, bool) {
	effort, found := ResponsesLogicalModelReasoningEffort(info)
	if !found {
		return request, false
	}

	updatedRequest := request
	if request.Reasoning == nil {
		updatedRequest.Reasoning = &dto.Reasoning{Effort: effort}
		return updatedRequest, true
	}
	updatedReasoning := *request.Reasoning
	updatedReasoning.Effort = effort
	updatedRequest.Reasoning = &updatedReasoning
	return updatedRequest, true
}

// ResponsesLogicalModelReasoningEffort resolves an OpenAI effort suffix from
// the original routed model. Compact routing appends its own suffix after the
// client model, so it must be removed before parsing the effort.
func ResponsesLogicalModelReasoningEffort(info *RelayInfo) (string, bool) {
	if info == nil {
		return "", false
	}
	model := strings.TrimSpace(info.OriginModelName)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		if baseModel, ok := ratio_setting.CompactBaseModelName(model); ok {
			model = baseModel
		}
	}
	effort, _ := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(model)
	return effort, effort != ""
}
