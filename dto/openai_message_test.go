package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAITextResponseChoiceEmbedsReasoningContentInMessage(t *testing.T) {
	reasoning := "thinking must be preserved"
	choice := OpenAITextResponseChoice{
		Index: 0,
		Message: Message{
			Role:             "assistant",
			ReasoningContent: &reasoning,
		},
		FinishReason: "tool_calls",
	}
	choice.SetStringContent("")

	encoded, err := common.Marshal(OpenAITextResponse{
		Id:      "chatcmpl-test",
		Model:   "deepseek-v4-flash",
		Choices: []OpenAITextResponseChoice{choice},
	})
	require.NoError(t, err)

	message := gjson.GetBytes(encoded, "choices.0.message")
	require.True(t, message.Exists())
	require.Equal(t, "assistant", message.Get("role").String())
	require.Equal(t, "", message.Get("content").String())
	require.Equal(t, reasoning, message.Get("reasoning_content").String())
	require.False(t, gjson.GetBytes(encoded, "choices.0.reasoning_content").Exists())
}

func TestMessageStringContentFromMediaContent(t *testing.T) {
	message := Message{
		Role: "assistant",
		Content: []MediaContent{
			{Type: ContentTypeText, Text: "alpha"},
			{Type: ContentTypeImageURL, ImageUrl: &MessageImageUrl{Url: "data:image/png;base64,AA=="}},
			{Type: ContentTypeText, Text: " beta"},
		},
	}

	require.Equal(t, "alpha beta", message.StringContent())
}

func TestMessageStringContentFromParsedContent(t *testing.T) {
	message := Message{
		Role: "assistant",
		parsedContent: []MediaContent{
			{Type: ContentTypeText, Text: "first"},
			{Type: ContentTypeImageURL, ImageUrl: &MessageImageUrl{Url: "data:image/png;base64,AA=="}},
			{Type: ContentTypeText, Text: " second"},
		},
	}

	require.Equal(t, "first second", message.StringContent())
}
