package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
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
	require.Len(t, items, 3)

	require.Equal(t, "developer", responseItemRole(t, items[0]))
	require.Equal(t, "assistant", responseItemRole(t, items[1]))
	require.Equal(t, "old answer", responseItemText(t, items[1]))
	require.Equal(t, "user", responseItemRole(t, items[2]))
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

func TestConvertOpenAIResponsesRequestPreservesAssistantAfterEncryptedReasoningWhenStripEnabled(t *testing.T) {
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

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 1)
	require.Equal(t, "assistant", responseItemRole(t, items[0]))
	require.Equal(t, "old answer", responseItemText(t, items[0]))
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

func TestConvertOpenAIResponsesRequestPreservesAssistantToolCallsWhenStripEnabled(t *testing.T) {
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
	require.Len(t, items, 5)
	require.Equal(t, "function_call", responseItemType(t, items[0]))
	require.Equal(t, "custom_tool_call", responseItemType(t, items[1]))
	require.Equal(t, "function_call_output", responseItemType(t, items[2]))
	require.Equal(t, "user", responseItemRole(t, items[3]))
	require.Equal(t, "assistant", responseItemRole(t, items[4]))
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

func TestConvertOpenAIResponsesRequestPreservesToolOutputWhenStripEnabled(t *testing.T) {
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
	require.Len(t, items, 3)
	require.Equal(t, "function_call", responseItemType(t, items[0]))
	require.Equal(t, "function_call_output", responseItemType(t, items[1]))
	require.Contains(t, string(items[1]["metadata"]), "internal")
	require.Equal(t, "user", responseItemRole(t, items[2]))
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

func TestConvertOpenAIResponsesCompactRequestPreservesOpenAICompatibleChannelByDefault(t *testing.T) {
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

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "resp_previous", convertedReq.PreviousResponseID)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
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

func TestConvertOpenAIResponsesCompactRequestBuildsSyntheticSummaryRequest(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
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
	require.NotContains(t, string(convertedReq.Input), "opaque")
	require.NotContains(t, string(convertedReq.Input), "Visible conversation to compact")
	require.NotContains(t, string(convertedReq.Input), "[user] continue")
	require.Contains(t, string(convertedReq.Input), "Use the existing previous_response_id context as the source of truth for the compaction.")
}

func TestConvertOpenAIResponsesCompactNativeModeRestoresSyntheticState(t *testing.T) {
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
	compactResp, _, err := service.BuildSyntheticCompactResponse(context.Background(), service.SyntheticCompactStateScope{}, "gpt-5.5", dto.OpenAIResponsesResponse{
		CreatedAt: 1710000000,
		Model:     "gpt-5.5",
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "Stored native switch summary."},
				},
			},
		},
	})
	require.NoError(t, err)
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: compactResp.ID,
		Input:              json.RawMessage(`"continue"`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Empty(t, convertedReq.PreviousResponseID)
	require.Contains(t, string(convertedReq.Input), "Stored native switch summary.")
}

func TestConvertOpenAIResponsesRequestRestoresSyntheticStateBeforeStrip(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:       dto.ResponsesCompactModeSynthetic,
				StripCodexEncryptedContext: true,
			},
		},
	}
	compactResp, _, err := service.BuildSyntheticCompactResponse(context.Background(), service.SyntheticCompactStateScope{}, "gpt-5.5", dto.OpenAIResponsesResponse{
		CreatedAt: 1710000000,
		Model:     "gpt-5.5",
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "Stored synthetic summary."},
				},
			},
		},
	})
	require.NoError(t, err)

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"},
			{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"old answer"}]},
			` + string(compactResp.Output)[1:len(compactResp.Output)-1] + `,
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Empty(t, convertedReq.PreviousResponseID)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 4)
	require.Equal(t, "developer", responseItemRole(t, items[0]))
	require.Contains(t, responseItemText(t, items[0]), "Stored synthetic summary.")
	require.Equal(t, "assistant", responseItemRole(t, items[1]))
	require.Equal(t, "old answer", responseItemText(t, items[1]))
	require.Equal(t, "user", responseItemRole(t, items[2]))
	require.Equal(t, "developer", responseItemRole(t, items[3]))
	require.Contains(t, responseItemText(t, items[3]), "Another language model produced the compact summary above")
	require.NotContains(t, string(convertedReq.Input), "opaque")
}

func TestConvertOpenAIResponsesCompactRequestStripsNativeCompactWhenEncryptedContextStripEnabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:       dto.ResponsesCompactModeNative,
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

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 2)
	require.Equal(t, "assistant", responseItemRole(t, items[0]))
	require.Equal(t, "old answer", responseItemText(t, items[0]))
	require.Equal(t, "user", responseItemRole(t, items[1]))
	require.NotContains(t, string(convertedReq.Input), "opaque")
	require.NotContains(t, string(convertedReq.Input), "compact")
}

func TestConvertOpenAIResponsesCompactRequestConvertsAndStripsEncryptedContextWhenEnabled(t *testing.T) {
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

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "resp_previous", convertedReq.PreviousResponseID)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 2)
	require.Equal(t, "assistant", responseItemRole(t, items[0]))
	require.Equal(t, "old answer", responseItemText(t, items[0]))
	require.Equal(t, "user", responseItemRole(t, items[1]))
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
	require.Len(t, items, 2)
	require.Equal(t, "assistant", responseItemRole(t, items[0]))
	require.Equal(t, "old answer", responseItemText(t, items[0]))
	require.Equal(t, "user", responseItemRole(t, items[1]))
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

func TestOpenAIResponsesCompactRequestURLUsesCompactEndpointByDefault(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeResponsesCompact,
		RequestURLPath: "/v1/responses/compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://api.example.test",
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.example.test/v1/responses/compact", requestURL)
}

func TestOpenAIResponsesCompactRequestURLUsesResponsesEndpointForSyntheticSummaryMode(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeResponsesCompact,
		RequestURLPath: "/v1/responses/compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://api.example.test",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.example.test/v1/responses", requestURL)
}

func responseItemRole(t *testing.T, item map[string]json.RawMessage) string {
	t.Helper()
	var role string
	require.NoError(t, common.Unmarshal(item["role"], &role))
	return role
}

func responseItemType(t *testing.T, item map[string]json.RawMessage) string {
	t.Helper()
	var itemType string
	require.NoError(t, common.Unmarshal(item["type"], &itemType))
	return itemType
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
