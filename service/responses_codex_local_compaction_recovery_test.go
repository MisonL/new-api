package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestBuildCodexLocalCompaction413RecoveryRequestUsesBoundedVisibleInput(t *testing.T) {
	stream := true
	largeImageData := strings.Repeat("A", 128*1024)
	request := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.6-sol",
		Conversation:       common.RawMessage(`"conv_should_not_be_replayed"`),
		ContextManagement:  common.RawMessage(`{"threshold":123}`),
		PreviousResponseID: "resp_should_not_be_replayed",
		Stream:             &stream,
		Tools:              common.RawMessage(`[{"type":"function","name":"tool_should_not_be_replayed"}]`),
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Summarize the active task."}]},
			{"type":"custom_tool_call_output","call_id":"call_image","output":[{"type":"input_image","image_url":"data:image/png;base64,%s"}]},
			{"type":"additional_tools","tools":[{"type":"custom","name":"additional_tool_should_not_be_replayed"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_TASK_MUST_SURVIVE"}]}
		]`, largeImageData)),
	}

	recovered, info, err := BuildCodexLocalCompaction413RecoveryRequest(request)

	require.NoError(t, err)
	require.Equal(t, request.Model, recovered.Model)
	require.NotNil(t, recovered.Stream)
	require.True(t, *recovered.Stream)
	require.Empty(t, recovered.Conversation)
	require.Empty(t, recovered.ContextManagement)
	require.Empty(t, recovered.PreviousResponseID)
	require.Empty(t, recovered.Tools)
	require.Contains(t, string(recovered.Input), "LATEST_TASK_MUST_SURVIVE")
	require.NotContains(t, string(recovered.Input), "data:image/png;base64,")
	require.NotContains(t, string(recovered.Input), "additional_tool_should_not_be_replayed")
	require.Less(t, len(recovered.Input), len(request.Input))
	require.Equal(t, len(request.Input), info.OriginalInputBytes)
	require.Equal(t, len(recovered.Input), info.RecoveredInputBytes)
	require.Greater(t, info.RecoveredRequestBytes, info.RecoveredInputBytes)
	require.True(t, info.EmbeddedMediaOmitted)

	var inputItems []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(recovered.Input, &inputItems))
	require.Len(t, inputItems, 1)
	var inputType string
	require.NoError(t, common.Unmarshal(inputItems[0]["type"], &inputType))
	require.Equal(t, "message", inputType)
}

func TestBuildCodexLocalCompaction413RecoveryRequestRejectsNonStream(t *testing.T) {
	stream := false
	_, _, err := BuildCodexLocalCompaction413RecoveryRequest(dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input:  common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]}]`),
	})

	require.ErrorContains(t, err, "requires a streaming request")
}

func TestBuildCodexLocalCompaction413RecoveryRequestRejectsOpaqueReplay(t *testing.T) {
	stream := true
	request := dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	recovered, info, err := BuildCodexLocalCompaction413RecoveryRequest(request)

	require.ErrorIs(t, err, ErrCodexLocalCompactionOpaqueEncryptedReplay)
	require.Equal(t, dto.OpenAIResponsesRequest{}, recovered)
	require.Equal(t, CodexLocalCompaction413RecoveryInfo{}, info)
}

func TestBuildCodexLocalCompaction413RecoveryRequestDropsAdditionalToolsFromUnknownFields(t *testing.T) {
	stream := true
	recovered, _, err := BuildCodexLocalCompaction413RecoveryRequest(dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input:  common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_CONTEXT"}]}]`),
		Extra: map[string]common.RawMessage{
			"additional_tools": common.RawMessage(`[{"type":"custom","name":"MUST_NOT_BE_REPLAYED"}]`),
			"future_option":    common.RawMessage(`{"enabled":true}`),
		},
	})

	require.NoError(t, err)
	require.NotContains(t, recovered.Extra, "additional_tools")
	require.JSONEq(t, `{"enabled":true}`, string(recovered.Extra["future_option"]))
	serialized, err := common.Marshal(recovered)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "MUST_NOT_BE_REPLAYED")
}

func TestBuildCodexLocalCompaction413RecoveryRequestStagesPreserveCompactionContract(t *testing.T) {
	stream := true
	richOnlyContext := "RICH_VISIBLE_CONTEXT_MUST_SURVIVE"
	latestContext := "LATEST_VISIBLE_CONTEXT_MUST_SURVIVE"
	request := dto.OpenAIResponsesRequest{
		Model:        "gpt-5.6-sol",
		Instructions: common.RawMessage(`"ORIGINAL_COMPACTION_INSTRUCTIONS_MUST_SURVIVE"`),
		Stream:       &stream,
		StreamOptions: &dto.StreamOptions{
			IncludeUsage: true,
			Extra: map[string]common.RawMessage{
				"reasoning_summary_delivery": common.RawMessage(`"detailed"`),
			},
		},
		Input: common.RawMessage(fmt.Sprintf(`"%s%s%s%s"`,
			strings.Repeat("A", syntheticCompactVisibleTextMax),
			richOnlyContext,
			strings.Repeat("B", syntheticCompactVisibleTextMax*2),
			latestContext,
		)),
	}

	rich, richInfo, err := BuildCodexLocalCompaction413RecoveryRequestForStage(request, CodexLocalCompaction413RecoveryStageRichVisible)
	require.NoError(t, err)
	bounded, boundedInfo, err := BuildCodexLocalCompaction413RecoveryRequestForStage(request, CodexLocalCompaction413RecoveryStageBoundedVisible)
	require.NoError(t, err)

	require.Equal(t, CodexLocalCompaction413RecoveryStageRichVisible, richInfo.Stage)
	require.Equal(t, CodexLocalCompaction413RecoveryStageBoundedVisible, boundedInfo.Stage)
	require.Equal(t, request.Instructions, rich.Instructions)
	require.Equal(t, request.Instructions, bounded.Instructions)
	require.NotNil(t, rich.StreamOptions)
	require.NotNil(t, bounded.StreamOptions)
	richStreamOptions, err := common.Marshal(rich.StreamOptions)
	require.NoError(t, err)
	boundedStreamOptions, err := common.Marshal(bounded.StreamOptions)
	require.NoError(t, err)
	require.Contains(t, string(richStreamOptions), "reasoning_summary_delivery")
	require.Contains(t, string(boundedStreamOptions), "reasoning_summary_delivery")
	require.Contains(t, string(rich.Input), richOnlyContext)
	require.NotContains(t, string(bounded.Input), richOnlyContext)
	require.Contains(t, string(rich.Input), latestContext)
	require.Contains(t, string(bounded.Input), latestContext)
	require.Greater(t, len(rich.Input), len(bounded.Input))
}

func TestBuildCodexLocalCompaction413RecoveryRequestPreservesExplicitFalseStreamOptionsOnFallback(t *testing.T) {
	stream := true
	var streamOptions dto.StreamOptions
	require.NoError(t, common.Unmarshal(common.RawMessage(`{"include_usage":false,"include_obfuscation":false}`), &streamOptions))
	streamOptions.Extra = map[string]common.RawMessage{
		"oversized_future_option": common.RawMessage(fmt.Sprintf(`%q`, strings.Repeat("x", codexLocalCompaction413RecoveryRawFieldMaxBytes))),
	}

	recovered, _, err := BuildCodexLocalCompaction413RecoveryRequest(dto.OpenAIResponsesRequest{
		Model:         "gpt-5.6-sol",
		Stream:        &stream,
		StreamOptions: &streamOptions,
		Input:         common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_CONTEXT"}]}]`),
	})

	require.NoError(t, err)
	serialized, err := common.Marshal(recovered)
	require.NoError(t, err)
	var payload struct {
		StreamOptions common.RawMessage `json:"stream_options"`
	}
	require.NoError(t, common.Unmarshal(serialized, &payload))
	require.JSONEq(t, `{"include_usage":false,"include_obfuscation":false}`, string(payload.StreamOptions))
}

func TestBuildCodexLocalCompaction413RecoveryBoundsModerateStreamOptionsExtra(t *testing.T) {
	stream := true
	var streamOptions dto.StreamOptions
	require.NoError(t, common.Unmarshal(common.RawMessage(`{"include_usage":true}`), &streamOptions))
	streamOptions.Extra = map[string]common.RawMessage{
		"moderate_future_option": common.RawMessage(fmt.Sprintf(`%q`, strings.Repeat("x", 96*1024))),
	}

	recovered, _, err := BuildCodexLocalCompaction413RecoveryRequest(dto.OpenAIResponsesRequest{
		Model:         "gpt-5.6-sol",
		Stream:        &stream,
		StreamOptions: &streamOptions,
		Input:         common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_CONTEXT"}]}]`),
	})

	require.NoError(t, err)
	require.NotNil(t, recovered.StreamOptions)
	require.True(t, recovered.StreamOptions.IncludeUsage)
	require.NotContains(t, recovered.StreamOptions.Extra, "moderate_future_option")
}

func TestBuildCodexLocalCompaction413RecoveryRequestPreservesExplicitEmptyReasoningFields(t *testing.T) {
	stream := true
	var reasoning dto.Reasoning
	require.NoError(t, common.Unmarshal(common.RawMessage(`{"effort":"","summary":""}`), &reasoning))

	recovered, _, err := BuildCodexLocalCompaction413RecoveryRequest(dto.OpenAIResponsesRequest{
		Model:     "gpt-5.6-sol",
		Stream:    &stream,
		Reasoning: &reasoning,
		Input:     common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_CONTEXT"}]}]`),
	})

	require.NoError(t, err)
	serialized, err := common.Marshal(recovered)
	require.NoError(t, err)
	var payload struct {
		Reasoning common.RawMessage `json:"reasoning"`
	}
	require.NoError(t, common.Unmarshal(serialized, &payload))
	require.JSONEq(t, `{"effort":"","summary":""}`, string(payload.Reasoning))
}

func TestBuildCodexLocalCompaction413RecoveryRequestPreservesExplicitNullReasoningAndStreamOptions(t *testing.T) {
	stream := true
	var reasoning dto.Reasoning
	require.NoError(t, common.Unmarshal(common.RawMessage(`{
		"mode":null,
		"context":null,
		"future_reasoning_option":null
	}`), &reasoning))
	var streamOptions dto.StreamOptions
	require.NoError(t, common.Unmarshal(common.RawMessage(`{
		"include_usage":null,
		"include_obfuscation":null,
		"reasoning_summary_delivery":null
	}`), &streamOptions))
	streamOptions.Extra["oversized_future_option"] = common.RawMessage(fmt.Sprintf(`%q`, strings.Repeat("x", codexLocalCompaction413RecoveryRawFieldMaxBytes)))

	recovered, _, err := BuildCodexLocalCompaction413RecoveryRequest(dto.OpenAIResponsesRequest{
		Model:         "gpt-5.6-sol",
		Stream:        &stream,
		Reasoning:     &reasoning,
		StreamOptions: &streamOptions,
		Input:         common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_CONTEXT"}]}]`),
	})

	require.NoError(t, err)
	serialized, err := common.Marshal(recovered)
	require.NoError(t, err)
	var payload struct {
		Reasoning     common.RawMessage `json:"reasoning"`
		StreamOptions common.RawMessage `json:"stream_options"`
	}
	require.NoError(t, common.Unmarshal(serialized, &payload))
	require.JSONEq(t, `{"mode":null,"context":null,"future_reasoning_option":null}`, string(payload.Reasoning))
	require.JSONEq(t, `{"include_usage":null,"include_obfuscation":null,"reasoning_summary_delivery":null}`, string(payload.StreamOptions))
}

func TestBoundedCodexCompactionRawFieldsUsesStableKeyOrder(t *testing.T) {
	values := map[string]common.RawMessage{
		"zeta":  common.RawMessage(`1`),
		"alpha": common.RawMessage(`2`),
		"beta":  common.RawMessage(`3`),
	}

	first := boundedCodexCompactionRawFields(values, len("alpha")+len("2")+len("beta")+len("3"))
	for i := 0; i < 20; i++ {
		require.Equal(t, first, boundedCodexCompactionRawFields(values, len("alpha")+len("2")+len("beta")+len("3")))
	}
	require.Equal(t, map[string]common.RawMessage{
		"alpha": common.RawMessage(`2`),
		"beta":  common.RawMessage(`3`),
	}, first)
}

func TestBoundedCodexCompactionRawFieldsUsesLexicographicSelectionWhenBudgetIsInsufficient(t *testing.T) {
	values := map[string]common.RawMessage{
		"b": common.RawMessage(`1`),
		"a": common.RawMessage(`2`),
	}

	got := boundedCodexCompactionRawFields(values, len("a")+len("2"))

	require.Equal(t, map[string]common.RawMessage{"a": common.RawMessage(`2`)}, got)
}
