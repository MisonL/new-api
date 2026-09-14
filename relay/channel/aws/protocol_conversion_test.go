package aws

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockdocument "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAwsApiKeyRequestURLUsesRegionModelAndStreamEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		IsStream: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "api-key|us-west-2",
			UpstreamModelName: "nova-lite-v1:0",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AwsKeyType: dto.AwsKeyTypeApiKey,
			},
		},
	}

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://bedrock-runtime.us-west-2.amazonaws.com/model/amazon.nova-lite-v1:0/converse-stream", url)
}

func TestAwsCrossRegionLookupAcceptsApacAlias(t *testing.T) {
	require.True(t, awsModelCanCrossRegion("amazon.nova-lite-v1:0", "ap"))
	require.True(t, awsModelCanCrossRegion("amazon.nova-lite-v1:0", "apac"))
	require.Equal(t, "apac.amazon.nova-lite-v1:0", awsModelCrossRegion("amazon.nova-lite-v1:0", "ap"))
	require.Equal(t, "apac.amazon.nova-lite-v1:0", awsModelCrossRegion("amazon.nova-lite-v1:0", "apac"))
}

func TestAwsHeaderOverrideAppliesDynamicHeadersToSDKRequest(t *testing.T) {
	request := smithyhttp.NewStackRequest().(*smithyhttp.Request)
	applyAwsHeaderOverrides(request, map[string]string{
		"x-codex-turn-metadata": "turn-1",
		"Host":                  "bedrock.example",
	})

	require.Equal(t, "turn-1", request.Header.Get("x-codex-turn-metadata"))
	require.Equal(t, "bedrock.example", request.Host)
}

func TestConvertOpenAIToConverseInputMapsToolsAndInference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	maxTokens := uint(256)
	temperature := 0.0
	converted, err := convertOpenAIToConverseInput(c, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
		Tools: []dto.ToolCallRequest{{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        "lookup",
				Description: "Look up a value",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
			},
		}},
		ToolChoice: "auto",
		Messages:   []dto.Message{{Role: "system", Content: "Policy"}, {Role: "user", Content: "Hello"}},
	}, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	require.NoError(t, err)
	require.Len(t, converted.System, 1)
	require.Len(t, converted.Messages, 1)
	require.NotNil(t, converted.InferenceConfig)
	require.NotNil(t, converted.InferenceConfig.Temperature)
	require.Equal(t, float32(0), *converted.InferenceConfig.Temperature)
	require.NotNil(t, converted.ToolConfig)
	require.Len(t, converted.ToolConfig.Tools, 1)
	require.IsType(t, &bedrocktypes.ToolChoiceMemberAuto{}, converted.ToolConfig.ToolChoice)
}

func TestConvertOpenAIToConverseInputRejectsThinkingContentAlias(t *testing.T) {
	reasoning := "provider-specific reasoning"
	_, err := convertOpenAIToConverseInput(nil, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "answer", ThinkingContent: &reasoning},
		},
	}, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	require.Error(t, err)
	require.Contains(t, err.Error(), "reasoning_content")
}

func TestConverseOutputToClaudeResponsePreservesReasoningAndToolUse(t *testing.T) {
	text := "answer"
	reasoning := "think"
	toolID := "call_1"
	toolName := "lookup"
	output := &bedrockruntime.ConverseOutput{
		Output: &bedrocktypes.ConverseOutputMemberMessage{Value: bedrocktypes.Message{
			Role: bedrocktypes.ConversationRoleAssistant,
			Content: []bedrocktypes.ContentBlock{
				&bedrocktypes.ContentBlockMemberReasoningContent{Value: &bedrocktypes.ReasoningContentBlockMemberReasoningText{Value: bedrocktypes.ReasoningTextBlock{Text: &reasoning}}},
				&bedrocktypes.ContentBlockMemberText{Value: text},
				&bedrocktypes.ContentBlockMemberToolUse{Value: bedrocktypes.ToolUseBlock{ToolUseId: &toolID, Name: &toolName, Input: bedrockdocument.NewLazyDocument(map[string]any{"q": "status"})}},
			},
		}},
		StopReason: bedrocktypes.StopReasonToolUse,
		Usage:      &bedrocktypes.TokenUsage{InputTokens: common.GetPointer[int32](10), OutputTokens: common.GetPointer[int32](4), TotalTokens: common.GetPointer[int32](14)},
	}
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := converseOutputToClaudeResponse(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude"}}, output)
	require.NoError(t, err)
	require.Len(t, converted.Content, 3)
	require.Equal(t, "thinking", converted.Content[0].Type)
	require.NotNil(t, converted.Content[0].Thinking)
	require.Equal(t, reasoning, *converted.Content[0].Thinking)
	require.Equal(t, text, converted.Content[1].GetText())
	require.Equal(t, "tool_use", converted.Content[2].Type)
	require.Equal(t, toolID, converted.Content[2].Id)
	require.Equal(t, 10, converted.Usage.InputTokens)
}

func TestConvertToNovaRequestPreservesSystemAndExplicitZeroValues(t *testing.T) {
	zero := 0.0
	maxTokens := uint(128)
	converted, err := convertToNovaRequest(&dto.GeneralOpenAIRequest{
		Temperature: &zero,
		MaxTokens:   &maxTokens,
		Messages: []dto.Message{
			{Role: "system", Content: "Policy."},
			{Role: "user", Content: "Hello"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []NovaContent{{Text: "Policy."}}, converted.System)
	require.Equal(t, []NovaMessage{{Role: "user", Content: []NovaContent{{Text: "Hello"}}}}, converted.Messages)
	require.NotNil(t, converted.InferenceConfig)
	require.NotNil(t, converted.InferenceConfig.Temperature)
	require.Equal(t, 0.0, *converted.InferenceConfig.Temperature)
	require.NotNil(t, converted.InferenceConfig.MaxTokens)
	require.Equal(t, 128, *converted.InferenceConfig.MaxTokens)
}

func TestConvertToNovaRequestRejectsNonTextContent(t *testing.T) {
	_, err := convertToNovaRequest(&dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "user",
		Content: common.RawMessage(`[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]`),
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "image_url")
}

func TestConvertToNovaRequestRejectsMalformedStop(t *testing.T) {
	_, err := convertToNovaRequest(&dto.GeneralOpenAIRequest{
		Stop:     []any{"ok", 1},
		Messages: []dto.Message{{Role: "user", Content: "Hello"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "stop[1]")
}

func TestConverseClaudeStreamProcessesMetadataAfterMessageStop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	state := &converseStreamState{
		ID:                  "msg_1",
		Model:               "claude",
		Usage:               &dto.Usage{},
		ClaudeUsage:         &dto.ClaudeUsage{},
		ClaudeBlocksStarted: make(map[int32]bool),
	}

	require.NoError(t, handleConverseClaudeStreamEvent(c, nil, state, &bedrocktypes.ConverseStreamOutputMemberMessageStart{
		Value: bedrocktypes.MessageStartEvent{Role: bedrocktypes.ConversationRoleAssistant},
	}))
	index := int32(0)
	require.NoError(t, handleConverseClaudeStreamEvent(c, nil, state, &bedrocktypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: bedrocktypes.ContentBlockDeltaEvent{ContentBlockIndex: &index, Delta: &bedrocktypes.ContentBlockDeltaMemberText{Value: "answer"}},
	}))
	require.NoError(t, handleConverseClaudeStreamEvent(c, nil, state, &bedrocktypes.ConverseStreamOutputMemberMessageStop{
		Value: bedrocktypes.MessageStopEvent{StopReason: bedrocktypes.StopReasonEndTurn},
	}))
	require.True(t, state.MessageStopped)
	require.False(t, state.MetadataReceived)
	require.NoError(t, handleConverseClaudeStreamEvent(c, nil, state, &bedrocktypes.ConverseStreamOutputMemberMetadata{
		Value: bedrocktypes.ConverseStreamMetadataEvent{Usage: &bedrocktypes.TokenUsage{
			InputTokens: common.GetPointer[int32](7), OutputTokens: common.GetPointer[int32](3), TotalTokens: common.GetPointer[int32](10),
		}},
	}))
	require.True(t, state.MetadataReceived)
	require.Equal(t, 7, state.ClaudeUsage.InputTokens)
	require.Equal(t, 3, state.ClaudeUsage.OutputTokens)
	require.Contains(t, recorder.Body.String(), "message_start")
}
