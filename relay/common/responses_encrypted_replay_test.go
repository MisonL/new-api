package common

import (
	"net/http/httptest"
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesEncryptedReasoningReplayActionLifecycle(t *testing.T) {
	tests := []struct {
		name            string
		capability      dto.ResponsesEncryptedReasoningReplayCapability
		selectedAction  string
		attemptedAction string
		forwardedAction string
	}{
		{
			name:            "supported",
			capability:      dto.ResponsesEncryptedReasoningReplayCapabilitySupported,
			selectedAction:  ResponsesEncryptedReasoningReplayActionSelectedNative,
			attemptedAction: ResponsesEncryptedReasoningReplayActionAttemptedNative,
			forwardedAction: ResponsesEncryptedReasoningReplayActionForwardedNative,
		},
		{
			name:            "unknown",
			capability:      dto.ResponsesEncryptedReasoningReplayCapabilityUnknown,
			selectedAction:  ResponsesEncryptedReasoningReplayActionSelectedNativeUnverified,
			attemptedAction: ResponsesEncryptedReasoningReplayActionAttemptedNativeUnverified,
			forwardedAction: ResponsesEncryptedReasoningReplayActionForwardedNativeUnverified,
		},
		{
			name:            "unsupported remains rejected",
			capability:      dto.ResponsesEncryptedReasoningReplayCapabilityUnsupported,
			selectedAction:  ResponsesEncryptedReasoningReplayActionRejectedUnsupported,
			attemptedAction: ResponsesEncryptedReasoningReplayActionRejectedUnsupported,
			forwardedAction: ResponsesEncryptedReasoningReplayActionRejectedUnsupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())

			MarkResponsesEncryptedReasoningReplayDecision(c, tt.capability)
			require.Equal(t, tt.selectedAction, basecommon.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))

			MarkResponsesEncryptedReasoningReplayAttempt(c)
			require.Equal(t, tt.attemptedAction, basecommon.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))

			MarkResponsesEncryptedReasoningReplayForwarded(c)
			require.Equal(t, tt.forwardedAction, basecommon.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
		})
	}
}

func TestSupportsNativeOpaqueResponsesCompact(t *testing.T) {
	supported := true
	unsupported := false
	tests := []struct {
		name        string
		channelType int
		settings    dto.ChannelOtherSettings
		want        bool
	}{
		{
			name:        "openai native default",
			channelType: constant.ChannelTypeOpenAI,
			want:        true,
		},
		{
			name:        "openai synthetic",
			channelType: constant.ChannelTypeOpenAI,
			settings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
		{
			name:        "openai disabled",
			channelType: constant.ChannelTypeOpenAI,
			settings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeDisabled,
			},
		},
		{
			name:        "codex native default",
			channelType: constant.ChannelTypeCodex,
			want:        true,
		},
		{
			name:        "codex explicitly unsupported",
			channelType: constant.ChannelTypeCodex,
			settings: dto.ChannelOtherSettings{
				ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
					SupportsResponsesCompact: &unsupported,
				},
			},
		},
		{
			name:        "codex synthetic",
			channelType: constant.ChannelTypeCodex,
			settings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
		{
			name:        "azure unknown",
			channelType: constant.ChannelTypeAzure,
		},
		{
			name:        "azure explicitly supported",
			channelType: constant.ChannelTypeAzure,
			settings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
				ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
					SupportsResponsesCompact: &supported,
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, SupportsNativeOpaqueResponsesCompact(tt.channelType, tt.settings))
		})
	}
}
