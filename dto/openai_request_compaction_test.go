package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestResponsesInputHasCompactionTriggerScansLargeHistoryWithoutChangingResult(t *testing.T) {
	largeToolOutput := strings.Repeat("A", 2*1024*1024)
	input, err := common.Marshal([]map[string]any{
		{
			"type":    "custom_tool_call_output",
			"call_id": "call_large_image",
			"output": []map[string]string{
				{"type": "input_image", "image_url": "data:image/png;base64," + largeToolOutput},
			},
		},
		{"type": ResponsesInputTypeCompactionTrigger},
	})
	require.NoError(t, err)

	require.True(t, ResponsesInputHasCompactionTrigger(input))
}

func TestResponsesInputHasCompactionTriggerRejectsMalformedArray(t *testing.T) {
	require.False(t, ResponsesInputHasCompactionTrigger(common.RawMessage(`[{"type":"compaction_trigger"},`)))
}
