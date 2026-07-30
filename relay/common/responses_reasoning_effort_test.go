package common

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestResponsesLogicalModelReasoningEffort(t *testing.T) {
	tests := []struct {
		name  string
		info  *RelayInfo
		want  string
		found bool
	}{
		{
			name:  "standard suffix",
			info:  &RelayInfo{OriginModelName: "gpt-5.6-sol-high"},
			want:  "high",
			found: true,
		},
		{
			name: "compact suffix",
			info: &RelayInfo{
				RelayMode:       relayconstant.RelayModeResponsesCompact,
				OriginModelName: "gpt-5.6-sol-low-openai-compact",
			},
			want:  "low",
			found: true,
		},
		{
			name:  "no suffix",
			info:  &RelayInfo{OriginModelName: "gpt-5.6-sol"},
			found: false,
		},
		{
			name:  "nil info",
			info:  nil,
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effort, found := ResponsesLogicalModelReasoningEffort(tt.info)
			require.Equal(t, tt.found, found)
			require.Equal(t, tt.want, effort)
		})
	}
}

func TestApplyResponsesLogicalModelReasoningEffortDoesNotMutateInput(t *testing.T) {
	originalReasoning := &dto.Reasoning{
		Effort:  "medium",
		Summary: "auto",
		Mode:    json.RawMessage(`"adaptive"`),
		Context: json.RawMessage(`"all_turns"`),
		Extra: map[string]json.RawMessage{
			"future_reasoning_option": json.RawMessage(`{"enabled":true}`),
		},
	}
	request := dto.OpenAIResponsesRequest{
		Model:     "provider/gpt-5.6-sol",
		Reasoning: originalReasoning,
	}
	info := &RelayInfo{OriginModelName: "gpt-5.6-sol-high"}

	updated, applied := ApplyResponsesLogicalModelReasoningEffort(info, request)

	require.True(t, applied)
	require.NotSame(t, request.Reasoning, updated.Reasoning)
	require.Equal(t, "medium", request.Reasoning.Effort)
	require.Equal(t, "high", updated.Reasoning.Effort)
	require.Equal(t, request.Model, updated.Model)
	require.Equal(t, request.Reasoning.Summary, updated.Reasoning.Summary)
	require.JSONEq(t, string(request.Reasoning.Mode), string(updated.Reasoning.Mode))
	require.JSONEq(t, string(request.Reasoning.Context), string(updated.Reasoning.Context))
	require.JSONEq(t, string(request.Reasoning.Extra["future_reasoning_option"]), string(updated.Reasoning.Extra["future_reasoning_option"]))
}

func TestApplyResponsesLogicalModelReasoningEffortPreservesRequestWithoutSuffix(t *testing.T) {
	request := dto.OpenAIResponsesRequest{Model: "provider/gpt-5.6-sol"}

	updated, applied := ApplyResponsesLogicalModelReasoningEffort(&RelayInfo{OriginModelName: "gpt-5.6-sol"}, request)

	require.False(t, applied)
	require.Equal(t, request, updated)
}
