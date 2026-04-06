package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

func TestApplyClaudeThinkingModeOmitsTopPForThinkingSuffix(t *testing.T) {
	claudeSettings := model_setting.GetClaudeSettings()
	origEnabled := claudeSettings.ThinkingAdapterEnabled
	origBudgetRatio := claudeSettings.ThinkingAdapterBudgetTokensPercentage
	globalSettings := model_setting.GetGlobalSettings()
	origBlacklist := append([]string(nil), globalSettings.ThinkingModelBlacklist...)
	t.Cleanup(func() {
		claudeSettings.ThinkingAdapterEnabled = origEnabled
		claudeSettings.ThinkingAdapterBudgetTokensPercentage = origBudgetRatio
		globalSettings.ThinkingModelBlacklist = origBlacklist
	})

	claudeSettings.ThinkingAdapterEnabled = true
	claudeSettings.ThinkingAdapterBudgetTokensPercentage = 0.8
	globalSettings.ThinkingModelBlacklist = nil

	request := &dto.ClaudeRequest{
		Model: "claude-3-5-sonnet-thinking",
		TopP:  common.GetPointer(0.2),
	}

	changed := applyClaudeThinkingMode(request, request.Model)
	require.True(t, changed)
	require.Nil(t, request.TopP)
	require.NotNil(t, request.Temperature)
	require.Equal(t, 1.0, *request.Temperature)
	require.NotNil(t, request.Thinking)
	require.Equal(t, "enabled", request.Thinking.Type)
	require.Equal(t, "claude-3-5-sonnet", request.Model)
}

func TestApplyClaudeThinkingModeOmitsTopPForOpusEffort(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model: "claude-opus-4-6-high",
		TopP:  common.GetPointer(0.2),
	}

	changed := applyClaudeThinkingMode(request, request.Model)
	require.True(t, changed)
	require.Nil(t, request.TopP)
	require.NotNil(t, request.Temperature)
	require.Equal(t, 1.0, *request.Temperature)
	require.NotNil(t, request.Thinking)
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.Equal(t, "claude-opus-4-6", request.Model)
	require.JSONEq(t, `{"effort":"high"}`, string(request.OutputConfig))
}

