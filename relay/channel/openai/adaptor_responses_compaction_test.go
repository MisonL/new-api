package openai

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestPreservesCompactionInputByDefault(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"keep prior summary"}]},
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
}

func TestConvertOpenAIResponsesRequestStripsCodexEncryptedContextWhenEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"keep prior summary"}]},
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},
			{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"old answer"}]},
			{"type":"compaction","encrypted_content":"compact"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 2)

	require.Equal(t, "developer", responseItemRole(t, items[0]))
	require.Equal(t, "user", responseItemRole(t, items[1]))
}

func TestConvertOpenAIResponsesRequestRejectsCompactionOnlyInputWhenStripEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[{"type":"compaction","encrypted_content":"opaque"}]`),
	}

	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compaction input")
}

func TestConvertOpenAIResponsesRequestPreservesNonObjectArrayItemsWhenStripEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			"plain input item",
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 2)
	var first string
	require.NoError(t, common.Unmarshal(items[0], &first))
	require.Equal(t, "plain input item", first)
}

func TestConvertOpenAIResponsesRequestRejectsEncryptedReasoningOnlyInputWhenStripEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},
			{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"old answer"}]}
		]`),
	}

	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "encrypted reasoning context")
}

func TestConvertOpenAIResponsesRequestPreservesNonAdjacentAssistantWhenStripEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"fresh answer"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 2)
	require.Equal(t, "user", responseItemRole(t, items[0]))
	require.Equal(t, "assistant", responseItemRole(t, items[1]))
	require.Equal(t, "fresh answer", responseItemText(t, items[1]))
}

func TestConvertOpenAIResponsesRequestStripsAssistantToolCallsUntilNextClientContext(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},
			{"type":"function_call","call_id":"call_1","name":"edit","arguments":"{}"},
			{"type":"custom_tool_call","call_id":"call_2","name":"shell","input":"go test"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"fresh answer"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 3)
	require.Equal(t, "user", responseItemRole(t, items[0]))
	require.Contains(t, responseItemText(t, items[0]), "Tool output call_1")
	require.Equal(t, "user", responseItemRole(t, items[1]))
	require.Equal(t, "assistant", responseItemRole(t, items[2]))
}

func TestConvertOpenAIResponsesRequestPreservesPlainReasoningWhenStripEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"visible reasoning"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"keep answer"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
}

func TestConvertOpenAIResponsesRequestPreservesEmptyEncryptedReasoningWhenStripEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","encrypted_content":""},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"keep answer"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
}

func TestConvertOpenAIResponsesRequestRewritesToolOutputWithoutRawFallback(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},
			{"type":"function_call","call_id":"call_1","name":"edit","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","metadata":{"secret":"internal"}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 2)
	require.Equal(t, "Tool output call_1", responseItemText(t, items[0]))
	require.NotContains(t, responseItemText(t, items[0]), "metadata")
	require.Equal(t, "user", responseItemRole(t, items[1]))
}

func TestConvertOpenAIResponsesCompactRequestPreservesCompactionInputForCodexChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeCodex,
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[{"type":"compaction","encrypted_content":"opaque"}]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
}

func TestConvertOpenAIResponsesCompactRequestRejectsOpenAICompatibleChannelWithoutNativeCompact(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_previous",
		Input: json.RawMessage(`[
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "responses compact")
	require.Contains(t, err.Error(), "native")
}

func TestConvertOpenAIResponsesCompactRequestPreservesNativeOpenAICompactSemantics(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_previous",
		Input: json.RawMessage(`[
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "resp_previous", convertedReq.PreviousResponseID)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
}

func TestConvertOpenAIResponsesCompactRequestKeepsNativeCompactWhenStripEncryptedContextEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:        dto.ResponsesCompactModeNative,
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_previous",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},
			{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"old answer"}]},
			{"type":"compaction","encrypted_content":"compact"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "resp_previous", convertedReq.PreviousResponseID)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
}

func TestConvertOpenAIResponsesCompactRequestRejectsUnsupportedChannelEvenWhenStripEncryptedContextEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_previous",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},
			{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"old answer"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "responses compact")
	require.Contains(t, err.Error(), "native")
}

func TestConvertAzureResponsesCompactRequestStripsEncryptedReasoningWhenEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-azure-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeAzure,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_previous",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},
			{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"old answer"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "resp_previous", convertedReq.PreviousResponseID)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 1)
	require.Equal(t, "user", responseItemRole(t, items[0]))
}

func TestOpenAIResponsesCompactRequestURLUsesCompactEndpointForNativeOpenAICompatibleChannel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeResponsesCompact,
		RequestURLPath: "/v1/responses/compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://api.example.test",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
			},
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.example.test/v1/responses/compact", requestURL)
}

func responseItemRole(t *testing.T, item map[string]json.RawMessage) string {
	t.Helper()
	var role string
	require.NoError(t, common.Unmarshal(item["role"], &role))
	return role
}

func responseItemText(t *testing.T, item map[string]json.RawMessage) string {
	t.Helper()
	var content []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(item["content"], &content))
	require.NotEmpty(t, content)
	var text string
	require.NoError(t, common.Unmarshal(content[0]["text"], &text))
	return text
}
