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

// Bedrock 的 thinking 块以一个 signature delta 收尾（SDK 注释明确要求把它
// 原样回传）。旧实现直接 return error，错误上抛到 converseStreamHandler 被
// 包成 502，而此刻 message_start 与 thinking_delta 已经 flush，客户端拿到
// 半截 SSE。
func TestConverseClaudeStreamSkipsReasoningSignatureDelta(t *testing.T) {
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
		Value: bedrocktypes.ContentBlockDeltaEvent{
			ContentBlockIndex: &index,
			Delta:             &bedrocktypes.ContentBlockDeltaMemberReasoningContent{Value: &bedrocktypes.ReasoningContentBlockDeltaMemberText{Value: "thinking..."}},
		},
	}))

	// 关键断言：signature delta 不能让整条流失败。
	require.NoError(t, handleConverseClaudeStreamEvent(c, nil, state, &bedrocktypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: bedrocktypes.ContentBlockDeltaEvent{
			ContentBlockIndex: &index,
			Delta:             &bedrocktypes.ContentBlockDeltaMemberReasoningContent{Value: &bedrocktypes.ReasoningContentBlockDeltaMemberSignature{Value: "sig-abc"}},
		},
	}))

	body := recorder.Body.String()
	require.Contains(t, body, "thinking_delta")
	require.NotContains(t, body, "sig-abc", "signature is opaque integrity metadata and must never reach the visible stream")
}

func TestConverseOpenAIStreamSkipsReasoningSignatureDelta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	state := &converseStreamState{
		ID:                "msg_1",
		Model:             "claude",
		Usage:             &dto.Usage{},
		OpenAIToolIndexes: make(map[int32]int),
	}

	index := int32(0)
	require.NoError(t, handleConverseOpenAIStreamEvent(c, nil, state, &bedrocktypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: bedrocktypes.ContentBlockDeltaEvent{
			ContentBlockIndex: &index,
			Delta:             &bedrocktypes.ContentBlockDeltaMemberReasoningContent{Value: &bedrocktypes.ReasoningContentBlockDeltaMemberText{Value: "thinking..."}},
		},
	}))
	require.NoError(t, handleConverseOpenAIStreamEvent(c, nil, state, &bedrocktypes.ConverseStreamOutputMemberContentBlockDelta{
		Value: bedrocktypes.ContentBlockDeltaEvent{
			ContentBlockIndex: &index,
			Delta:             &bedrocktypes.ContentBlockDeltaMemberReasoningContent{Value: &bedrocktypes.ReasoningContentBlockDeltaMemberSignature{Value: "sig-abc"}},
		},
	}))
	require.NotContains(t, recorder.Body.String(), "sig-abc")
}

// Claude 请求里逐块的 cache_control 经 service.ClaudeToOpenAIRequest 保留到
// MediaContent.CacheControl，但 Converse 转换只取 Text 就把它丢了，全仓也
// 从不发 cachePoint。结果是 200 OK 但上游永不建缓存，后续每轮按全价输入
// 计费，客户端无感知。同一文件对顶层 cache_control 反而显式报错，两种处理
// 自相矛盾。
func TestConvertOpenAIToConverseInputMapsBlockCacheControlToCachePoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	content, err := common.Marshal([]map[string]any{
		{"type": "text", "text": "long stable prefix", "cache_control": map[string]any{"type": "ephemeral"}},
	})
	require.NoError(t, err)

	converted, err := convertOpenAIToConverseInput(c, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: common.RawMessage(content)}},
	}, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	require.NoError(t, err)
	require.Len(t, converted.Messages, 1)

	var cachePoints int
	for _, block := range converted.Messages[0].Content {
		if _, ok := block.(*bedrocktypes.ContentBlockMemberCachePoint); ok {
			cachePoints++
		}
	}
	require.Equal(t, 1, cachePoints,
		"a text block carrying cache_control must emit a cachePoint block")
}

func TestConverseCachePointRejectsUnknownTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	content, err := common.Marshal([]map[string]any{
		{"type": "text", "text": "x", "cache_control": map[string]any{"type": "ephemeral", "ttl": "24h"}},
	})
	require.NoError(t, err)
	_, err = convertOpenAIToConverseInput(c, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: common.RawMessage(content)}},
	}, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ttl")
}

func TestConverseCachePointMapsOneHourTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	content, err := common.Marshal([]map[string]any{
		{"type": "text", "text": "x", "cache_control": map[string]any{"type": "ephemeral", "ttl": "1h"}},
	})
	require.NoError(t, err)
	converted, err := convertOpenAIToConverseInput(c, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: common.RawMessage(content)}},
	}, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	require.NoError(t, err)
	var found bool
	for _, block := range converted.Messages[0].Content {
		if cp, ok := block.(*bedrocktypes.ContentBlockMemberCachePoint); ok {
			found = true
			require.Equal(t, bedrocktypes.CacheTTLOneHour, cp.Value.Ttl)
		}
	}
	require.True(t, found)
}

// Bedrock 的 TokenUsage 带 cacheReadInputTokens / cacheWriteInputTokens /
// cacheDetails（SDK v1.63.7 types.go 有），但 converseUsage 只读 input/output，
// CachedTokens 与 CacheReadInputTokens 恒 0。UsageSemantic 写死 openai 后，
// service/text_quota.go 的 openai 分支把整段输入按全价计，cache_ratio 永远
// 不生效；同渠道的 AKSK/InvokeModel 路径反而是对的。
func TestConverseUsageMapsCacheTokens(t *testing.T) {
	usage := &bedrocktypes.TokenUsage{
		InputTokens:           common.GetPointer[int32](100),
		OutputTokens:          common.GetPointer[int32](20),
		TotalTokens:           common.GetPointer[int32](120),
		CacheReadInputTokens:  common.GetPointer[int32](40),
		CacheWriteInputTokens: common.GetPointer[int32](10),
		CacheDetails: []bedrocktypes.CacheDetail{
			{InputTokens: common.GetPointer[int32](4), Ttl: bedrocktypes.CacheTTLFiveMinutes},
			{InputTokens: common.GetPointer[int32](6), Ttl: bedrocktypes.CacheTTLOneHour},
		},
	}

	converted := converseUsage(usage)
	require.Equal(t, 40, converted.PromptTokensDetails.CachedTokens,
		"cache read tokens must reach the billing input details")
	require.Equal(t, 10, converted.PromptTokensDetails.CachedCreationTokens,
		"cache write tokens must reach the billing input details")
	require.Equal(t, 4, converted.ClaudeCacheCreation5mTokens)
	require.Equal(t, 6, converted.ClaudeCacheCreation1hTokens)

	claudeUsage := converseClaudeUsage(usage)
	require.Equal(t, 40, claudeUsage.CacheReadInputTokens)
	require.Equal(t, 10, claudeUsage.CacheCreationInputTokens)
}

// Converse 的 Claude 响应把 thinking 文本发给客户端（不带 signature，因为
// OpenAI 形状没有载体）。客户端（Claude Code 等）下一轮会原样回传，转换后
// 成为 reasoning_content，而旧实现无条件拒绝它 —— 一条自己产出、自己拒绝的
// 链路，多轮思考对话必然 400。
//
// SDK 的 ReasoningTextBlock.Signature 是可选字段；这里改为把 reasoning_content
// 映射回 ReasoningContent 块（text 原样），不再在网关侧直接失败。
func TestConvertOpenAIToConverseInputAcceptsReasoningContentReplay(t *testing.T) {
	reasoning := "provider-specific reasoning"
	converted, err := convertOpenAIToConverseInput(nil, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "answer", ReasoningContent: &reasoning},
			{Role: "user", Content: "next"},
		},
	}, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	require.NoError(t, err)
	require.Len(t, converted.Messages, 3)

	var found *bedrocktypes.ContentBlockMemberReasoningContent
	for _, block := range converted.Messages[1].Content {
		if rc, ok := block.(*bedrocktypes.ContentBlockMemberReasoningContent); ok {
			found = rc
		}
	}
	require.NotNil(t, found, "reasoning_content must map back to a ReasoningContent block")
	text, ok := found.Value.(*bedrocktypes.ReasoningContentBlockMemberReasoningText)
	require.True(t, ok)
	require.NotNil(t, text.Value.Text)
	require.Equal(t, reasoning, *text.Value.Text)
}
