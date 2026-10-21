package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// Claude 入站请求经 OpenAI 兼容渠道转发时，工具定义必须原样到达上游。
// 转换链是 ClaudeToOpenAIRequest -> Adaptor.ConvertOpenAIRequest，中间还有
// 一层 adaptor 处理（SenseNova 规范化、StreamOptions、OpenRouter 分支）。
// 这条测试钉住这一层的边界：任何在 adaptor 里清空 tools 的改动都会让
// 客户端以为模型没有工具可用，而错误只表现为"模型不调用工具"，很难发现。
func TestConvertClaudeRequestPreservesToolsThroughOpenAIAdaptor(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    "https://api.example.com/v1",
			UpstreamModelName: "gpt-4o",
		},
	}
	request := &dto.ClaudeRequest{
		Model: "gpt-4o",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "read the file"},
		},
		Tools: []any{
			map[string]any{
				"name":         "Read",
				"description":  "read a file",
				"input_schema": map[string]any{"type": "object"},
			},
			map[string]any{
				"name":         "Write",
				"description":  "write a file",
				"input_schema": map[string]any{"type": "object"},
			},
		},
	}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, info, request)
	require.NoError(t, err)
	got, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok, "expected *dto.GeneralOpenAIRequest, got %T", converted)

	require.Len(t, got.Tools, 2, "adaptor must not drop Claude tools on the way to an OpenAI upstream")
	require.Equal(t, "function", got.Tools[0].Type)
	require.Equal(t, "Read", got.Tools[0].Function.Name)
	require.Equal(t, "Write", got.Tools[1].Function.Name)
}
