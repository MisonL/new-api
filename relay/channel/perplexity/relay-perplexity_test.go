package perplexity

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2PerplexityPreservesMessageMetadata(t *testing.T) {
	name := "speaker"
	reasoning := "internal"
	toolCalls := []byte(`[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]`)
	converted := requestOpenAI2Perplexity(dto.GeneralOpenAIRequest{
		Model: "sonar",
		Messages: []dto.Message{{
			Role:             "assistant",
			Content:          "answer",
			Name:             &name,
			ReasoningContent: &reasoning,
			ToolCalls:        toolCalls,
		}},
	})
	require.Equal(t, "answer", converted.Messages[0].Content)
	require.Equal(t, &name, converted.Messages[0].Name)
	require.Equal(t, &reasoning, converted.Messages[0].ReasoningContent)
	require.Equal(t, string(toolCalls), string(converted.Messages[0].ToolCalls))
}
