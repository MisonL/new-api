package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestTokenCountMetaIncludesEffectiveAdditionalTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Tools: common.RawMessage(`[{"type":"function","name":"top_level_tool","description":"TOP_LEVEL_TOOL_MARKER","parameters":{"type":"object"}}]`),
		Input: common.RawMessage(`[
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"custom","name":"additional_tool","description":"ADDITIONAL_TOOL_MARKER","format":{"type":"text"}}
			]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	meta := req.GetTokenCountMeta()

	require.Contains(t, meta.CombineText, "TOP_LEVEL_TOOL_MARKER")
	require.Contains(t, meta.CombineText, "ADDITIONAL_TOOL_MARKER")
	require.Equal(t, 2, meta.ToolsCount)
	require.Equal(t, 1, strings.Count(meta.CombineText, "TOP_LEVEL_TOOL_MARKER"))
	require.Equal(t, 1, strings.Count(meta.CombineText, "ADDITIONAL_TOOL_MARKER"))
}

func TestOpenAIResponsesCompactionRequestTokenCountMetaIncludesEffectiveToolsAndMaxOutputTokens(t *testing.T) {
	maxOutputTokens := uint(2048)
	req := &OpenAIResponsesCompactionRequest{
		MaxOutputTokens: &maxOutputTokens,
		Tools:           common.RawMessage(`[{"type":"function","name":"top_level_tool","description":"TOP_LEVEL_COMPACT_TOOL_MARKER","parameters":{"type":"object"}}]`),
		Input: common.RawMessage(`[
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"custom","name":"additional_tool","description":"ADDITIONAL_COMPACT_TOOL_MARKER","format":{"type":"text"}}
			]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]}
		]`),
	}

	meta := req.GetTokenCountMeta()

	require.Equal(t, int(maxOutputTokens), meta.MaxTokens)
	require.Equal(t, 2, meta.ToolsCount)
	require.Contains(t, meta.CombineText, "TOP_LEVEL_COMPACT_TOOL_MARKER")
	require.Contains(t, meta.CombineText, "ADDITIONAL_COMPACT_TOOL_MARKER")
}
