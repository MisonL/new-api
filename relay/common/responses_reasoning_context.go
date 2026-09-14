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

const (
	ResponsesReasoningContextActionForwardedSupported  = "forwarded_supported_capability"
	ResponsesReasoningContextActionForwardedUnverified = "forwarded_unverified_capability"
	ResponsesReasoningContextActionStrippedUnsupported = "stripped_unsupported_capability"
)

type ResponsesReasoningContextCapabilityDecision struct {
	Capability  dto.ResponsesReasoningContextCapability
	Action      string
	ChannelID   int
	Model       string
	Profile     dto.ResponsesUpstreamProfile
	ChannelType int
	Settings    dto.ChannelOtherSettings
}

func ResolveResponsesReasoningContextCapabilityDecision(info *RelayInfo, requestModel string) ResponsesReasoningContextCapabilityDecision {
	decision := ResponsesReasoningContextCapabilityDecision{
		Capability: dto.ResponsesReasoningContextCapabilityUnknown,
		Action:     ResponsesReasoningContextActionForwardedUnverified,
		Model:      requestModel,
	}
	if info != nil {
		if info.OriginModelName != "" {
			decision.Model = info.OriginModelName
		}
		if info.ChannelMeta != nil {
			decision.ChannelID = info.ChannelId
			decision.ChannelType = info.ChannelType
			decision.Settings = info.ChannelOtherSettings
		}
	}
	decision.Profile = decision.Settings.NormalizedResponsesUpstreamProfile()
	decision.Capability = decision.Settings.ResolveResponsesReasoningContextCapability(decision.ChannelType)
	if decision.Capability == dto.ResponsesReasoningContextCapabilitySupported {
		decision.Action = ResponsesReasoningContextActionForwardedSupported
	} else if decision.Capability == dto.ResponsesReasoningContextCapabilityUnsupported {
		decision.Action = ResponsesReasoningContextActionStrippedUnsupported
	}
	return decision
}

// ApplyResponsesReasoningContextCapability applies the channel's explicit
// reasoning.context capability decision before a Responses request is sent.
func ApplyResponsesReasoningContextCapability(c *gin.Context, info *RelayInfo, request dto.OpenAIResponsesRequest) dto.OpenAIResponsesRequest {
	if request.Reasoning == nil || len(bytes.TrimSpace(request.Reasoning.Context)) == 0 {
		return request
	}
	decision := ResolveResponsesReasoningContextCapabilityDecision(info, request.Model)
	if decision.Capability == dto.ResponsesReasoningContextCapabilityUnsupported {
		strippedRequest, stripped := StripResponsesReasoningContext(request)
		if stripped {
			request = strippedRequest
			basecommon.SysLog(fmt.Sprintf(
				"responses reasoning context stripped by channel capability: channel_id=%d model=%s profile=%s",
				decision.ChannelID,
				decision.Model,
				decision.Profile,
			))
		}
	}
	if c != nil {
		basecommon.SetContextKey(c, constant.ContextKeyResponsesReasoningContextAction, decision.Action)
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
