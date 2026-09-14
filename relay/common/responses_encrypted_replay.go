package common

import (
	"errors"
	"fmt"
	"net/http"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

var ErrResponsesEncryptedReasoningReplayUnsupported = errors.New("responses encrypted reasoning replay unsupported")
var ErrResponsesOpaqueReplayParamOverrideUnsupported = errors.New("responses opaque replay param override unsupported")

const (
	ResponsesEncryptedReasoningReplayActionSelectedNative            = "selected_native"
	ResponsesEncryptedReasoningReplayActionSelectedNativeUnverified  = "selected_native_unverified"
	ResponsesEncryptedReasoningReplayActionAttemptedNative           = "attempted_native"
	ResponsesEncryptedReasoningReplayActionAttemptedNativeUnverified = "attempted_native_unverified"
	ResponsesEncryptedReasoningReplayActionForwardedNative           = "forwarded_native"
	ResponsesEncryptedReasoningReplayActionForwardedNativeUnverified = "forwarded_native_unverified"
	ResponsesEncryptedReasoningReplayActionRejectedUnsupported       = "rejected_unsupported"
	ResponsesEncryptedReasoningReplayRetrySuppressedCrossChannel     = "suppressed_cross_channel"
)

func IsOpaqueResponsesReplayRequest(request dto.Request) bool {
	_, ok := request.(*dto.OpaqueOpenAIResponsesRequest)
	return ok
}

func MarkResponsesEncryptedReasoningReplayDecision(c *gin.Context, capability dto.ResponsesEncryptedReasoningReplayCapability) {
	if c == nil {
		return
	}
	basecommon.SetContextKey(c, constant.ContextKeyResponsesOpaqueEncryptedReplay, true)
	basecommon.SetContextKey(c, constant.ContextKeyResponsesEncryptedReasoningReplayCapability, string(capability))
	action := ResponsesEncryptedReasoningReplayActionSelectedNativeUnverified
	switch capability {
	case dto.ResponsesEncryptedReasoningReplayCapabilitySupported:
		action = ResponsesEncryptedReasoningReplayActionSelectedNative
	case dto.ResponsesEncryptedReasoningReplayCapabilityUnsupported:
		action = ResponsesEncryptedReasoningReplayActionRejectedUnsupported
	}
	basecommon.SetContextKey(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction, action)
}

func IsResponsesOpaqueEncryptedReplay(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if basecommon.GetContextKeyBool(c, constant.ContextKeyResponsesOpaqueEncryptedReplay) {
		return true
	}
	metadata, ok := basecommon.GetResponsesRequestMetadata(c)
	return ok && metadata.HasOpaqueEncryptedContent
}

func MarkResponsesEncryptedReasoningReplayAttempt(c *gin.Context) {
	markResponsesEncryptedReasoningReplayForwardingState(c, ResponsesEncryptedReasoningReplayActionAttemptedNative, ResponsesEncryptedReasoningReplayActionAttemptedNativeUnverified)
}

func MarkResponsesEncryptedReasoningReplayRejected(c *gin.Context) {
	if !IsResponsesOpaqueEncryptedReplay(c) {
		return
	}
	basecommon.SetContextKey(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction, ResponsesEncryptedReasoningReplayActionRejectedUnsupported)
}

func MarkResponsesEncryptedReasoningReplayForwarded(c *gin.Context) {
	markResponsesEncryptedReasoningReplayForwardingState(c, ResponsesEncryptedReasoningReplayActionForwardedNative, ResponsesEncryptedReasoningReplayActionForwardedNativeUnverified)
}

func markResponsesEncryptedReasoningReplayForwardingState(c *gin.Context, supportedAction string, unknownAction string) {
	if !IsResponsesOpaqueEncryptedReplay(c) {
		return
	}
	capability := dto.ResponsesEncryptedReasoningReplayCapability(basecommon.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
	switch capability {
	case dto.ResponsesEncryptedReasoningReplayCapabilitySupported:
		basecommon.SetContextKey(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction, supportedAction)
	case dto.ResponsesEncryptedReasoningReplayCapabilityUnknown:
		basecommon.SetContextKey(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction, unknownAction)
	}
}

func MarkResponsesEncryptedReasoningReplayRetrySuppressed(c *gin.Context) {
	if !IsResponsesOpaqueEncryptedReplay(c) {
		return
	}
	basecommon.SetContextKey(c, constant.ContextKeyResponsesEncryptedReasoningReplayRetryAction, ResponsesEncryptedReasoningReplayRetrySuppressedCrossChannel)
}

func NewResponsesEncryptedReasoningReplayUnsupportedError(channelID int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		fmt.Errorf(
			"%w: channel #%d cannot safely replay opaque encrypted reasoning state over native Responses",
			ErrResponsesEncryptedReasoningReplayUnsupported,
			channelID,
		),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func NewResponsesOpaqueReplayParamOverrideUnsupportedError(channelID int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: channel #%d configures a param override that cannot preserve opaque encrypted Responses request bytes", ErrResponsesOpaqueReplayParamOverrideUnsupported, channelID),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func SupportsNativeOpaqueResponsesCompact(channelType int, settings dto.ChannelOtherSettings) bool {
	if !settings.HasNativeResponsesCompact() {
		return false
	}
	capability := settings.ResolveResponsesChannelCapability(channelType)
	if capability.SupportsResponsesCompact {
		return true
	}
	if channelType != constant.ChannelTypeCodex {
		return false
	}
	if registry := settings.ResponsesCapabilityRegistry; registry != nil && registry.SupportsResponsesCompact != nil {
		return *registry.SupportsResponsesCompact
	}
	return true
}
