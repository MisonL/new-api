package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestConvertOpenAIResponsesRequestPreservesInputAudio(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"input_audio","input_audio":{"data":"DIRECT_AUDIO_DATA","format":"wav"}},
			{"type":"message","role":"user","content":[{"type":"input_audio","input_audio":{"data":"MESSAGE_AUDIO_DATA","format":"mp3"}}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
}

func TestConvertOpenAIResponsesRequestRemovesUnprefixedFunctionCallItemID(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"function_call","id":"iteminvalidprefix","call_id":"call_probe","name":"get_probe_status","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_probe","output":"probe-ok"},
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
	_, hasID := items[0]["id"]
	require.False(t, hasID)
	require.JSONEq(t, `{"type":"function_call","call_id":"call_probe","name":"get_probe_status","arguments":"{}"}`, string(mustMarshalRawMap(t, items[0])))
	require.Equal(t, 1, common.GetContextKeyInt(c, constant.ContextKeyResponsesInputItemIDNormalizationRemovedCount))
}

func TestConvertOpenAIResponsesRequestPreservesOpaqueReasoningIDAndNormalizesToolID(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"clientreasoningid","encrypted_content":"opaque-reasoning-state"},
			{"type":"function_call","id":"clientfunctionid","call_id":"call_probe","name":"exec","arguments":"{}"},
			{"type":"message","id":"msg_valid","role":"user","content":[]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 3)
	var reasoningID string
	require.NoError(t, common.Unmarshal(items[0]["id"], &reasoningID))
	require.Equal(t, "clientreasoningid", reasoningID)
	_, hasFunctionID := items[1]["id"]
	require.False(t, hasFunctionID)
	var callID string
	require.NoError(t, common.Unmarshal(items[1]["call_id"], &callID))
	require.Equal(t, "call_probe", callID)
	var messageID string
	require.NoError(t, common.Unmarshal(items[2]["id"], &messageID))
	require.Equal(t, "msg_valid", messageID)
	require.Equal(t, 1, common.GetContextKeyInt(c, constant.ContextKeyResponsesInputItemIDNormalizationRemovedCount))
}

func TestConvertOpenAIResponsesRequestAppliesReasoningContextCapability(t *testing.T) {
	tests := []struct {
		name        string
		relayMode   int
		profile     dto.ResponsesUpstreamProfile
		wantContext bool
		wantAction  string
	}{
		{
			name:       "sub2api http responses strips unsupported context",
			relayMode:  relayconstant.RelayModeResponses,
			profile:    dto.ResponsesUpstreamProfileSub2APIHTTP,
			wantAction: "stripped_unsupported_capability",
		},
		{
			name:       "sub2api http compact strips unsupported context",
			relayMode:  relayconstant.RelayModeResponsesCompact,
			profile:    dto.ResponsesUpstreamProfileSub2APIHTTP,
			wantAction: "stripped_unsupported_capability",
		},
		{
			name:        "sub2api websocket forwards supported context",
			relayMode:   relayconstant.RelayModeResponses,
			profile:     dto.ResponsesUpstreamProfileSub2APIWSV2,
			wantContext: true,
			wantAction:  "forwarded_supported_capability",
		},
		{
			name:        "generic openai forwards unverified context",
			relayMode:   relayconstant.RelayModeResponses,
			profile:     dto.ResponsesUpstreamProfileGenericOpenAI,
			wantContext: true,
			wantAction:  "forwarded_unverified_capability",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			info := &relaycommon.RelayInfo{
				RelayMode:       tt.relayMode,
				OriginModelName: "gpt-5.6-sol",
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeOpenAI,
					ChannelOtherSettings: dto.ChannelOtherSettings{
						ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
						ResponsesUpstreamProfile: tt.profile,
					},
				},
			}
			request := dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]`),
				Reasoning: &dto.Reasoning{
					Effort:  "high",
					Summary: "auto",
					Mode:    json.RawMessage(`"adaptive"`),
					Context: json.RawMessage(`"all_turns"`),
					Extra: map[string]json.RawMessage{
						"future_reasoning_option": json.RawMessage(`{"enabled":true}`),
					},
				},
			}

			converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, request)

			require.NoError(t, err)
			convertedRequest, ok := converted.(dto.OpenAIResponsesRequest)
			require.True(t, ok)
			require.NotNil(t, convertedRequest.Reasoning)
			require.Equal(t, tt.wantContext, len(convertedRequest.Reasoning.Context) > 0)
			require.Equal(t, "high", convertedRequest.Reasoning.Effort)
			require.Equal(t, "auto", convertedRequest.Reasoning.Summary)
			require.JSONEq(t, `"adaptive"`, string(convertedRequest.Reasoning.Mode))
			require.JSONEq(t, `{"enabled":true}`, string(convertedRequest.Reasoning.Extra["future_reasoning_option"]))
			require.JSONEq(t, `"all_turns"`, string(request.Reasoning.Context))
			require.Equal(t, tt.wantAction, common.GetContextKeyString(c, constant.ContextKeyResponsesReasoningContextAction))
		})
	}
}

func mustMarshalRawMap(t *testing.T, item map[string]json.RawMessage) []byte {
	t.Helper()
	raw, err := common.Marshal(item)
	require.NoError(t, err)
	return raw
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
				{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
			{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"old answer"}]},
				{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 4)

	require.Equal(t, "developer", responseItemRole(t, items[0]))
	require.Equal(t, "assistant", responseItemRole(t, items[1]))
	require.Equal(t, "old answer", responseItemText(t, items[1]))
	require.Equal(t, "compaction", responseItemType(t, items[2]))
	require.Equal(t, "user", responseItemRole(t, items[3]))
}

func TestConvertOpenAIResponsesRequestPreservesCompactionOnlyInputWhenStripEnabled(t *testing.T) {
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

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.JSONEq(t, string(req.Input), string(convertedReq.Input))
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
	require.Len(t, items, 3)
	var first string
	require.NoError(t, common.Unmarshal(items[0], &first))
	require.Equal(t, "plain input item", first)
	require.Contains(t, string(items[1]), `"type":"compaction"`)
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
				{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
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
				{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
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
				{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
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
				{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
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

func TestConvertOpenAIResponsesCompactRequestPreservesOpaqueReplayInsteadOfBuildingSyntheticSummary(t *testing.T) {
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
	require.Equal(t, req.Input, convertedReq.Input)
	require.Contains(t, string(convertedReq.Input), `"encrypted_content":"opaque"`)
	require.NotContains(t, string(convertedReq.Input), "Visible conversation to compact")
}

func TestConvertOpenAIResponsesRequestBuildsRemoteCompactionFallbackSummary(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyResponsesRemoteCompactSyntheticFallback, true)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Preserve the current task and decisions."}]},
			{"type":"compaction_trigger"}
		]`),
		Stream: common.GetPointer(true),
	}
	info.Request = &req

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Nil(t, convertedReq.Stream)
	require.Contains(t, string(convertedReq.Input), "Preserve the current task and decisions.")
	require.Contains(t, string(convertedReq.Input), "self-contained handoff summary")
	require.NotContains(t, string(convertedReq.Input), "compaction_trigger")
}

func TestSetupRequestHeaderForwardsRemoteCompactionV2Feature(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Accept", "text/event-stream")
	c.Request.Header.Set("X-Codex-Beta-Features", "memories,remote_compaction_v2")
	request := &dto.OpenAIResponsesRequest{
		Input: json.RawMessage(`[{"type":"compaction_trigger"}]`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		IsStream:  true,
		Request:   request,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	header := http.Header{}

	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	require.Equal(t, "memories,remote_compaction_v2", header.Get("X-Codex-Beta-Features"))

	c.Request.Header.Del("X-Codex-Beta-Features")
	c.Request.Header.Add("X-Codex-Beta-Features", "memories")
	c.Request.Header.Add("X-Codex-Beta-Features", "REMOTE_COMPACTION_V2,network_proxy")
	header = http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	require.Equal(t, []string{"memories", "REMOTE_COMPACTION_V2,network_proxy"}, header.Values("X-Codex-Beta-Features"))

	request.Input = json.RawMessage(`[{"type":"message","role":"user","content":"hello"}]`)
	header = http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	require.Empty(t, header.Get("X-Codex-Beta-Features"))
}

func TestConvertOpenAIResponsesCompactNativeModeRestoresSyntheticState(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	scope := service.SyntheticCompactStateScope{UserID: 10, TokenID: 20, Group: "default"}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		UserId:          scope.UserID,
		TokenId:         scope.TokenID,
		UsingGroup:      scope.Group,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
			},
		},
	}
	compactResp, _, err := service.BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5.5", dto.OpenAIResponsesResponse{
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
	require.Equal(t, "cleared_by_synthetic_restore", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
}

func TestConvertOpenAIResponsesRequestMarksMissingSyntheticState(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_newapi_synthcmp_22222222222222222222222222222222",
		Input:              json.RawMessage(`[]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.Nil(t, converted)
	require.ErrorIs(t, err, service.ErrSyntheticCompactStateNotFound)
	require.Equal(t, "missing_local_synthetic_state", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
}

func TestConvertOpenAIResponsesRequestContinuesMissingSyntheticStateWithVisibleInput(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_newapi_synthcmp_22222222222222222222222222222222",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue after stale local compact"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Empty(t, convertedReq.PreviousResponseID)
	require.Contains(t, string(convertedReq.Input), "continue after stale local compact")
	require.Equal(t, "stale_local_synthetic_state_visible_only", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactVisibleOnlyFallbackAttempted))
}

func TestConvertOpenAIResponsesRequestRestoresSyntheticStateBeforeStrip(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	scope := service.SyntheticCompactStateScope{UserID: 10, TokenID: 20, Group: "default"}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		UserId:          scope.UserID,
		TokenId:         scope.TokenID,
		UsingGroup:      scope.Group,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:       dto.ResponsesCompactModeSynthetic,
				StripCodexEncryptedContext: true,
			},
		},
	}
	compactResp, _, err := service.BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5.5", dto.OpenAIResponsesResponse{
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
				{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
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
	require.Len(t, items, 3)
	require.Equal(t, "developer", responseItemRole(t, items[0]))
	require.Contains(t, responseItemText(t, items[0]), "Stored synthetic summary.")
	require.Contains(t, responseItemText(t, items[0]), "Another language model produced the compact summary above")
	require.Equal(t, "assistant", responseItemRole(t, items[1]))
	require.Equal(t, "old answer", responseItemText(t, items[1]))
	require.Equal(t, "user", responseItemRole(t, items[2]))
	require.NotContains(t, string(convertedReq.Input), "opaque")
}

func TestConvertOpenAIResponsesCompactRequestPreservesNativeCompactWhenEncryptedContextStripEnabled(t *testing.T) {
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
			{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
			{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"old answer"}]},
				{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
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
	require.Len(t, items, 3)
	require.Equal(t, "assistant", responseItemRole(t, items[0]))
	require.Equal(t, "old answer", responseItemText(t, items[0]))
	require.Equal(t, "compaction", responseItemType(t, items[1]))
	require.Equal(t, "user", responseItemRole(t, items[2]))
	require.NotContains(t, string(convertedReq.Input), "opaque")
	require.Contains(t, string(convertedReq.Input), "compact")
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
				{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
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

func TestConvertOpenAIResponsesCompactRequestSub2APIHTTPPreservesNativeWithoutPreviousID(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
				{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]},
				{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Empty(t, convertedReq.PreviousResponseID)
	require.NotContains(t, string(convertedReq.Input), "opaque")
	require.Contains(t, string(convertedReq.Input), "compact")
	var items []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(convertedReq.Input, &items))
	require.Len(t, items, 2)
	require.Equal(t, "user", responseItemRole(t, items[0]))
	require.Equal(t, "compaction", responseItemType(t, items[1]))
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
				{"type":"reasoning","id":"rs_1","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
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

func TestConvertOpenAIResponsesRequestRejectsSub2APIHTTPPreviousResponseID(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_upstream_previous",
		Input:              json.RawMessage(`"continue"`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.ErrorContains(t, err, "previous_response_id is not supported by upstream profile sub2api_http over REST")
	require.Nil(t, converted)
	require.Equal(t, "rejected_by_upstream_profile", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
}

func TestConvertOpenAIResponsesCompactRequestRejectsSub2APIHTTPPreviousResponseIDInNativeMode(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_upstream_previous",
		Input:              json.RawMessage(`"compact"`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.ErrorContains(t, err, "previous_response_id is not supported by upstream profile sub2api_http over REST")
	require.Nil(t, converted)
	require.Equal(t, "rejected_by_upstream_profile", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
}

func TestConvertOpenAIResponsesCompactRequestForcesVisibleOnlyForSub2APIHTTPSyntheticMode(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeSynthetic,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_upstream_previous",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"compact visible history"}]}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Empty(t, convertedReq.PreviousResponseID)
	require.Contains(t, string(convertedReq.Input), "Visible conversation to compact")
	require.Contains(t, string(convertedReq.Input), "compact visible history")
	require.NotContains(t, string(convertedReq.Input), "existing previous_response_id context")
	require.NotContains(t, string(convertedReq.Input), "resp_upstream_previous")
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactVisibleOnly))
	require.Equal(t, "cleared_by_upstream_profile", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
}

func TestConvertOpenAIResponsesRequestMarksForwardedPreviousResponseID(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
			},
		},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_upstream_previous",
		Input:              json.RawMessage(`"continue"`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Equal(t, "resp_upstream_previous", convertedReq.PreviousResponseID)
	require.Equal(t, "forwarded_upstream", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
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

func TestOpenAIResponsesCompactRequestURLUsesCompactEndpointForSub2APIHTTPProfile(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeResponsesCompact,
		RequestURLPath: "/v1/responses/compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://api.example.test",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.example.test/v1/responses/compact", requestURL)
}

func TestOpenAIResponsesCompactAutoUsesResponsesEndpointDuringActiveFallbackWindow(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeResponsesCompact,
		RequestURLPath: "/v1/responses/compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://api.example.test",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:           dto.ResponsesCompactModeAuto,
				ResponsesCompactAutoFallbackAt: time.Now().Unix(),
			},
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.example.test/v1/responses", requestURL)
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

func TestOpenAIResponsesCompactRequestURLUsesResponsesEndpointForResponsesProxyProfile(t *testing.T) {
	for _, profile := range []dto.ResponsesUpstreamProfile{
		dto.ResponsesUpstreamProfileGenericProxy,
		dto.ResponsesUpstreamProfileChatOnlyProxy,
	} {
		info := &relaycommon.RelayInfo{
			RelayMode:      relayconstant.RelayModeResponsesCompact,
			RequestURLPath: "/v1/responses/compact",
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType:    constant.ChannelTypeOpenAI,
				ChannelBaseUrl: "https://api.example.test",
				ChannelOtherSettings: dto.ChannelOtherSettings{
					ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
					ResponsesUpstreamProfile: profile,
				},
			},
		}

		requestURL, err := (&Adaptor{}).GetRequestURL(info)
		require.NoError(t, err)
		require.Equal(t, "https://api.example.test/v1/responses", requestURL)
	}
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
