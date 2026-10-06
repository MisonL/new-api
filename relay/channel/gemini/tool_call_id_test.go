package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// When Gemini omits functionCall.id, the converter synthesises one from the
// candidate index and a per-call ordinal. Both are reset for every chunk, so
// two different tool calls arriving in two different chunks both become
// "call_gemini_0_0". The stream handler then keys its index map by that ID
// (GeminiChatStreamHandler, toolCallIndexByChoice) and treats the second call
// as a continuation of the first -- the client sees one tool call where the
// model made two, and the second call's arguments overwrite the first's.

func geminiChunkWithCall(name string, args map[string]any) *dto.GeminiChatResponse {
	return &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Index: 0,
			Content: dto.GeminiChatContent{
				Role: "model",
				Parts: []dto.GeminiPart{
					{FunctionCall: &dto.FunctionCall{FunctionName: name, Arguments: args}},
				},
			},
		}},
	}
}

// TestStreamGeminiToolCallIDsAreUniqueAcrossChunks is the regression: distinct
// tool calls in distinct chunks must get distinct IDs.
func TestStreamGeminiToolCallIDsAreUniqueAcrossChunks(t *testing.T) {
	first, _, err := streamResponseGeminiChat2OpenAIWithError(
		geminiChunkWithCall("lookup", map[string]any{"q": "first"}))
	require.NoError(t, err)
	second, _, err := streamResponseGeminiChat2OpenAIWithError(
		geminiChunkWithCall("submit", map[string]any{"q": "second"}))
	require.NoError(t, err)

	require.Len(t, first.Choices, 1)
	require.Len(t, second.Choices, 1)
	require.Len(t, first.Choices[0].Delta.ToolCalls, 1)
	require.Len(t, second.Choices[0].Delta.ToolCalls, 1)

	firstID := first.Choices[0].Delta.ToolCalls[0].ID
	secondID := second.Choices[0].Delta.ToolCalls[0].ID

	require.NotEmpty(t, firstID)
	require.NotEmpty(t, secondID)
	require.NotEqual(t, firstID, secondID,
		"不同 chunk 的两个 functionCall 不得共用同一个合成 ID")
}

// TestStreamGeminiToolCallsWithinOneChunkStayDistinct is the reverse check: the
// ordinal must still separate two calls inside a single chunk.
func TestStreamGeminiToolCallsWithinOneChunkStayDistinct(t *testing.T) {
	response := &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Index: 0,
			Content: dto.GeminiChatContent{
				Role: "model",
				Parts: []dto.GeminiPart{
					{FunctionCall: &dto.FunctionCall{FunctionName: "a", Arguments: map[string]any{}}},
					{FunctionCall: &dto.FunctionCall{FunctionName: "b", Arguments: map[string]any{}}},
				},
			},
		}},
	}

	converted, _, err := streamResponseGeminiChat2OpenAIWithError(response)
	require.NoError(t, err)
	require.Len(t, converted.Choices[0].Delta.ToolCalls, 2)

	first := converted.Choices[0].Delta.ToolCalls[0]
	second := converted.Choices[0].Delta.ToolCalls[1]
	require.NotEqual(t, first.ID, second.ID, "同一 chunk 内的两个调用必须有不同 ID")
	require.NotNil(t, first.Index)
	require.NotNil(t, second.Index)
	require.Equal(t, 0, *first.Index)
	require.Equal(t, 1, *second.Index)
}

// TestStreamGeminiToolCallKeepsUpstreamID pins that an ID the upstream did send
// is still passed through untouched.
func TestStreamGeminiToolCallKeepsUpstreamID(t *testing.T) {
	response := &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Index: 0,
			Content: dto.GeminiChatContent{
				Role: "model",
				Parts: []dto.GeminiPart{
					{FunctionCall: &dto.FunctionCall{
						ID: "call_upstream_given", FunctionName: "lookup", Arguments: map[string]any{},
					}},
				},
			},
		}},
	}

	converted, _, err := streamResponseGeminiChat2OpenAIWithError(response)
	require.NoError(t, err)
	require.Equal(t, "call_upstream_given", converted.Choices[0].Delta.ToolCalls[0].ID)
}
