package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestEffectiveToolsMergesTopLevelAndAdditionalTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Tools: common.RawMessage(`[{"type":"function","name":"lookup"}]`),
		Input: common.RawMessage(`[
			"plain input",
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"custom","name":"exec"},
				{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"send_message"}]}
			]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	tools, err := req.EffectiveTools()

	require.NoError(t, err)
	require.Len(t, tools, 3)
	require.JSONEq(t, `{"type":"function","name":"lookup"}`, string(tools[0]))
	require.JSONEq(t, `{"type":"custom","name":"exec"}`, string(tools[1]))
	require.JSONEq(t, `{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"send_message"}]}`, string(tools[2]))
}

func TestOpenAIResponsesRequestEffectiveToolsRejectsMalformedAdditionalTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[{"type":"additional_tools","tools":{"type":"function","name":"lookup"}}]`),
	}

	_, err := req.EffectiveTools()

	require.Error(t, err)
	require.Contains(t, err.Error(), "additional_tools.tools must be an array")
}
