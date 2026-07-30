package common

import (
	"bytes"
	"fmt"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

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
