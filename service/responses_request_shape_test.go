package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoIncludesRedactedResponsesRequestShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyRequestBodySize, int64(345678))

	stream := true
	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"additional_tools","role":"developer","tools":[
					{"type":"function","name":"lookup","parameters":{"type":"object"}},
					{"type":"custom","name":"apply_patch","format":{"type":"grammar","syntax":"lark","definition":"secret grammar"}},
					{"type":"namespace","name":"collaboration","tools":[]},
					{"type":"tool_search","name":"search"}
				]},
				{"type":"function_call","id":"fc_secret","call_id":"call_secret","name":"lookup","arguments":"secret arguments"},
				{"type":"function_call_output","call_id":"call_secret","output":"secret output"},
				{"type":"compaction_trigger"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"secret prompt"}]}
			]`),
			Tools: common.RawMessage(`[
				{"type":"function","name":"top_level","parameters":{"type":"object"}},
				{"type":"web_search_preview"}
			]`),
			ToolChoice:         common.RawMessage(`{"type":"function","name":"lookup"}`),
			PreviousResponseID: "resp_secret",
			Reasoning: &dto.Reasoning{
				Effort:  "xhigh",
				Summary: "auto",
				Mode:    common.RawMessage(`"responses"`),
				Context: common.RawMessage(`"all_turns"`),
			},
			Include:           common.RawMessage(`["reasoning.encrypted_content"]`),
			ParallelToolCalls: common.RawMessage(`true`),
			Store:             common.RawMessage(`false`),
			Stream:            &stream,
		},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, -1, -1)
	shape, ok := other["responses_request_shape"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, int64(345678), shape["body_bytes"])
	require.Equal(t, "array", shape["input_json_type"])
	require.Equal(t, 5, shape["input_item_count"])
	require.Equal(t, map[string]int{
		"additional_tools":     1,
		"compaction_trigger":   1,
		"function_call":        1,
		"function_call_output": 1,
		"message":              1,
	}, shape["input_item_types"])
	require.Equal(t, 6, shape["effective_tool_count"])
	require.Equal(t, map[string]int{
		"custom":             1,
		"function":           2,
		"namespace":          1,
		"tool_search":        1,
		"web_search_preview": 1,
	}, shape["effective_tool_types"])
	require.Equal(t, map[string]int{"grammar": 1}, shape["custom_tool_format_types"])
	require.Equal(t, map[string]int{"lark": 1}, shape["custom_tool_grammar_syntaxes"])
	require.Equal(t, true, shape["has_function_tools"])
	require.Equal(t, true, shape["has_custom_tools"])
	require.Equal(t, true, shape["has_namespace_tools"])
	require.Equal(t, true, shape["has_tool_search"])
	require.Equal(t, true, shape["has_previous_response_id"])
	require.Equal(t, true, shape["has_compaction_trigger"])
	require.Equal(t, true, shape["has_reasoning"])
	require.Equal(t, true, shape["has_reasoning_context"])
	require.Equal(t, true, shape["has_reasoning_mode"])
	require.Equal(t, true, shape["has_include"])
	require.Equal(t, true, shape["has_tool_choice"])
	require.Equal(t, true, shape["has_parallel_tool_calls"])
	require.Equal(t, true, shape["has_store"])
	require.Equal(t, true, shape["stream"])

	encoded, err := common.Marshal(shape)
	require.NoError(t, err)
	for _, secret := range []string{
		"secret prompt",
		"secret grammar",
		"secret arguments",
		"secret output",
		"resp_secret",
		"fc_secret",
		"call_secret",
		"lookup",
		"apply_patch",
	} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestGenerateTextOtherInfoOmitsResponsesShapeForNonResponsesRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyRequestBodySize, int64(42))

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		Request:     &dto.GeneralOpenAIRequest{Model: "gpt-5.6-sol"},
	}, 1, 1, 1, 0, 0, -1, -1)

	require.NotContains(t, other, "responses_request_shape")
}

func TestGenerateTextOtherInfoIncludesShapeForResponsesCompactionRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses/compact", nil)

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		Request: &dto.OpenAIResponsesCompactionRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"unknown_secret_type","content":"secret"},
				{"type":"compaction_trigger"}
			]`),
			Tools: common.RawMessage(`[
				{"type":"custom","name":"secret_name","format":{"type":"secret_format","syntax":"secret_syntax","definition":"secret grammar"}},
				{"type":"secret_tool_type","name":"secret_name"}
			]`),
		},
	}, 1, 1, 1, 0, 0, -1, -1)

	shape, ok := other["responses_request_shape"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, map[string]int{"compaction_trigger": 1, "other": 1}, shape["input_item_types"])
	require.Equal(t, map[string]int{"custom": 1, "other": 1}, shape["effective_tool_types"])
	require.Equal(t, map[string]int{"other": 1}, shape["custom_tool_format_types"])
	require.Equal(t, map[string]int{"other": 1}, shape["custom_tool_grammar_syntaxes"])
	require.Equal(t, true, shape["has_compaction_trigger"])

	encoded, err := common.Marshal(shape)
	require.NoError(t, err)
	for _, secret := range []string{
		"unknown_secret_type",
		"secret_tool_type",
		"secret_format",
		"secret_syntax",
		"secret_name",
		"secret grammar",
	} {
		require.NotContains(t, string(encoded), secret)
	}
}
