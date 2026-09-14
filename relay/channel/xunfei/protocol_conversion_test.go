package xunfei

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2XunfeiMapsTopKAndConvertsLeadingSystem(t *testing.T) {
	topK := 8
	converted, err := requestOpenAI2Xunfei(dto.GeneralOpenAIRequest{
		Model: "spark-max",
		TopK:  &topK,
		Messages: []dto.Message{
			{Role: "system", Content: "Policy."},
			{Role: "user", Content: "Hello"},
		},
	}, "app", "general")
	require.NoError(t, err)
	require.Equal(t, 8, converted.Parameter.Chat.TopK)
	require.Equal(t, []XunfeiMessage{
		{Role: "user", Content: "Policy."},
		{Role: "assistant", Content: "Okay"},
		{Role: "user", Content: "Hello"},
	}, converted.Payload.Message.Text)
}

func TestRequestOpenAI2XunfeiRejectsUnsupportedTopP(t *testing.T) {
	topP := 0.5
	_, err := requestOpenAI2Xunfei(dto.GeneralOpenAIRequest{
		Model: "spark-max",
		TopP:  &topP,
		Messages: []dto.Message{{
			Role: "user", Content: "Hello",
		}},
	}, "app", "general")
	require.Error(t, err)
	require.Contains(t, err.Error(), "sampling")
}

func TestRequestOpenAI2XunfeiRejectsUnrepresentableToolCall(t *testing.T) {
	_, err := requestOpenAI2Xunfei(dto.GeneralOpenAIRequest{Model: "spark-max", Messages: []dto.Message{{
		Role: "assistant", Content: "calling", ToolCalls: []byte(`[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]`),
	}}}, "app", "general")
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool_calls")
}
