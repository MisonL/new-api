package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func senseNovaDeepSeekInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    "https://token.sensenova.cn/v1",
			UpstreamModelName: model,
		},
	}
}

func TestConvertOpenAIRequestNormalizesSenseNovaDeepSeekFlashToolHistory(t *testing.T) {
	info := senseNovaDeepSeekInfo("deepseek-v4-flash")
	request := &dto.GeneralOpenAIRequest{
		Model:           "deepseek-v4-flash",
		ReasoningEffort: "max",
		Messages: []dto.Message{{
			Role: "assistant",
		}},
	}
	request.Messages[0].SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_1",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: "{}",
		},
	}})

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	got := converted.(*dto.GeneralOpenAIRequest)
	require.Equal(t, "high", got.ReasoningEffort, "Flash has no documented max effort")
	require.Empty(t, got.THINKING)
	require.NotNil(t, got.Messages[0].ReasoningContent, "thinking-enabled tool rounds need an explicit field")
	require.Empty(t, *got.Messages[0].ReasoningContent)
	require.Nil(t, got.Messages[0].ThinkingContent)
	serialized, err := common.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(serialized), `"reasoning_content":""`)
}

func TestConvertOpenAIRequestNormalizesSenseNovaDeepSeekProThinkingContent(t *testing.T) {
	info := senseNovaDeepSeekInfo("deepseek-v4-pro")
	reasoning := "preserve this tool-turn reasoning"
	request := &dto.GeneralOpenAIRequest{
		Model: "deepseek-v4-pro",
		Messages: []dto.Message{{
			Role:             "assistant",
			ReasoningContent: &reasoning,
		}},
	}
	request.Messages[0].SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_1",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: "{}",
		},
	}})

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	got := converted.(*dto.GeneralOpenAIRequest)
	require.Equal(t, reasoning, got.Messages[0].GetReasoningContent())
	require.Nil(t, got.Messages[0].ReasoningContent)
	require.NotNil(t, got.Messages[0].ThinkingContent)
	require.Equal(t, reasoning, *got.Messages[0].ThinkingContent)
	require.Empty(t, got.ReasoningEffort)
}

func TestConvertClaudeRequestMapsSenseNovaDeepSeekFlashThinkingAndDisabledMode(t *testing.T) {
	info := senseNovaDeepSeekInfo("deepseek-v4-flash")
	info.RelayFormat = "claude"
	request := &dto.ClaudeRequest{
		Model:    "deepseek-v4-flash",
		Thinking: &dto.Thinking{Type: "disabled"},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "look this up"},
			{Role: "assistant", Content: []dto.ClaudeMediaMessage{{
				Type:  "tool_use",
				Id:    "call_1",
				Name:  "lookup",
				Input: map[string]any{},
			}}},
		},
	}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, info, request)
	require.NoError(t, err)
	got := converted.(*dto.GeneralOpenAIRequest)
	require.Equal(t, "none", got.ReasoningEffort)
	require.Empty(t, got.THINKING)
	require.Nil(t, got.Messages[1].ReasoningContent, "disabled thinking must not fabricate reasoning history")
}

func TestConvertClaudeRequestMapsSenseNovaDeepSeekProThinkingType(t *testing.T) {
	info := senseNovaDeepSeekInfo("deepseek-v4-pro")
	request := &dto.ClaudeRequest{
		Model:        "deepseek-v4-pro",
		Thinking:     &dto.Thinking{Type: "adaptive"},
		OutputConfig: []byte(`{"effort":"xhigh"}`),
		Messages:     []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, info, request)
	require.NoError(t, err)
	got := converted.(*dto.GeneralOpenAIRequest)
	require.Equal(t, "high", got.ReasoningEffort, "SenseNova maps Claude xhigh to Pro high")
	require.JSONEq(t, `{"type":"adaptive"}`, string(got.THINKING))
}

func TestSenseNovaDeepSeekThinkingContentIsReadableThroughCommonAccessor(t *testing.T) {
	var response dto.OpenAITextResponse
	err := common.Unmarshal([]byte(`{"choices":[{"message":{"role":"assistant","thinking_content":"provider reasoning"}}]}`), &response)
	require.NoError(t, err)
	require.Equal(t, "provider reasoning", response.Choices[0].Message.GetReasoningContent())

	var stream dto.ChatCompletionsStreamResponse
	err = common.Unmarshal([]byte(`{"choices":[{"delta":{"thinking_content":"stream reasoning"}}]}`), &stream)
	require.NoError(t, err)
	require.Equal(t, "stream reasoning", stream.Choices[0].Delta.GetReasoningContent())
}
