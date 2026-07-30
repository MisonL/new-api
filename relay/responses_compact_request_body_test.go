package relay

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	openairelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupResponsesCompactRequestBodyTest(t *testing.T, rawBody []byte) (*gin.Context, *dto.OpenAIResponsesRequest) {
	t.Helper()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	var compactRequest dto.OpenAIResponsesCompactionRequest
	require.NoError(t, common.Unmarshal(rawBody, &compactRequest))
	return c, compactRequest.ToResponsesRequest()
}

func newResponsesCompactPassThroughInfo(settings dto.ChannelOtherSettings) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
			ChannelOtherSettings: settings,
		},
	}
}

func TestBuildOpenAIResponsesCompactRequestBodyPreservesNativeRequestExceptMappedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{
  "model": "gpt-5.6-sol",
  "input": [{"type":"message","id":"msg_1","role":"user","content":[{"type":"input_text","text":"compact"}]}],
  "reasoning": {"context":"all_turns","mode":"adaptive","future_reasoning_option":{"enabled":true}},
  "stream_options": {"reasoning_summary_delivery":"sequential_cutoff"},
  "future_compact_option": {"enabled":true}
}`)
	c, request := setupResponsesCompactRequestBodyTest(t, rawBody)
	request.Model = "provider/gpt-5.6-sol"
	info := newResponsesCompactPassThroughInfo(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	expectedBody := bytes.Replace(rawBody, []byte(`"gpt-5.6-sol"`), []byte(`"provider/gpt-5.6-sol"`), 1)
	require.Equal(t, expectedBody, bodyBytes)
	require.Equal(t, "provider/gpt-5.6-sol", converted.Model)
	require.NotNil(t, converted.Reasoning)
	require.JSONEq(t, `"all_turns"`, string(converted.Reasoning.Context))
	require.JSONEq(t, `"adaptive"`, string(converted.Reasoning.Mode))
	require.JSONEq(t, `{"enabled":true}`, string(converted.Reasoning.Extra["future_reasoning_option"]))
	require.Equal(t, "forwarded_supported_capability", common.GetContextKeyString(c, constant.ContextKeyResponsesReasoningContextAction))
}

func TestBuildOpenAIResponsesCompactRequestBodyAppliesReasoningContextCapability(t *testing.T) {
	unsupported := false
	tests := []struct {
		name        string
		settings    dto.ChannelOtherSettings
		wantContext bool
		wantAction  string
		wantRaw     bool
	}{
		{
			name: "supported profile preserves raw body",
			settings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
			},
			wantContext: true,
			wantAction:  "forwarded_supported_capability",
			wantRaw:     true,
		},
		{
			name: "unknown profile preserves raw body",
			settings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericOpenAI,
			},
			wantContext: true,
			wantAction:  "forwarded_unverified_capability",
			wantRaw:     true,
		},
		{
			name: "explicitly unsupported strips only context",
			settings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
				ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
					SupportsReasoningContext: &unsupported,
				},
			},
			wantAction: "stripped_unsupported_capability",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawBody := []byte(`{"model":"gpt-5.6-sol","input":"compact","reasoning":{"effort":"high","context":"all_turns","mode":"adaptive","future_reasoning_option":{"enabled":true}},"future_compact_option":{"enabled":true}}`)
			c, request := setupResponsesCompactRequestBodyTest(t, rawBody)
			info := newResponsesCompactPassThroughInfo(tt.settings)

			body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

			require.Nil(t, apiErr)
			bodyBytes, err := io.ReadAll(body)
			require.NoError(t, err)
			require.Equal(t, tt.wantRaw, bytes.Equal(rawBody, bodyBytes))
			require.NotNil(t, converted.Reasoning)
			require.Equal(t, tt.wantContext, len(converted.Reasoning.Context) > 0)
			require.Equal(t, "high", converted.Reasoning.Effort)
			require.JSONEq(t, `"adaptive"`, string(converted.Reasoning.Mode))
			require.JSONEq(t, `{"enabled":true}`, string(converted.Reasoning.Extra["future_reasoning_option"]))
			require.Equal(t, tt.wantAction, common.GetContextKeyString(c, constant.ContextKeyResponsesReasoningContextAction))

			var bodyMap map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(bodyBytes, &bodyMap))
			require.JSONEq(t, `{"enabled":true}`, string(bodyMap["future_compact_option"]))
		})
	}
}

func TestBuildOpenAIResponsesCompactRequestBodyConvertsModelEffortSuffixWithoutReasoningContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.6-sol-high","input":"compact"}`)
	c, request := setupResponsesCompactRequestBodyTest(t, rawBody)
	info := newResponsesCompactPassThroughInfo(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})
	info.OriginModelName = "gpt-5.6-sol-high"

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NotEqual(t, rawBody, bodyBytes)
	require.Equal(t, "gpt-5.6-sol", converted.Model)
	require.NotNil(t, converted.Reasoning)
	require.Equal(t, "high", converted.Reasoning.Effort)
	require.Equal(t, "high", info.ReasoningEffort)
	require.Contains(t, string(bodyBytes), `"model":"gpt-5.6-sol"`)
	require.Contains(t, string(bodyBytes), `"effort":"high"`)
}

func TestBuildOpenAIResponsesCompactRequestBodyPreservesHeaderOnlyOverrideAndConvertsBodyOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":"compact","reasoning":{"context":"all_turns","future_reasoning_option":{"enabled":true}}}`)

	t.Run("header only override preserves body", func(t *testing.T) {
		c, request := setupResponsesCompactRequestBodyTest(t, rawBody)
		info := newResponsesCompactPassThroughInfo(dto.ChannelOtherSettings{
			ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
		})
		info.RequestHeaders = map[string]string{"x-codex-session-id": "session-live"}
		info.ParamOverride = map[string]interface{}{
			"operations": []interface{}{map[string]interface{}{
				"mode":  "pass_headers",
				"value": []interface{}{"x-codex-session-id"},
			}},
		}

		body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

		require.Nil(t, apiErr)
		bodyBytes, err := io.ReadAll(body)
		require.NoError(t, err)
		require.Equal(t, rawBody, bodyBytes)
		require.True(t, info.UseRuntimeHeadersOverride)
		require.Equal(t, "session-live", info.RuntimeHeadersOverride["x-codex-session-id"])
	})

	t.Run("body override uses conversion path", func(t *testing.T) {
		c, request := setupResponsesCompactRequestBodyTest(t, rawBody)
		info := newResponsesCompactPassThroughInfo(dto.ChannelOtherSettings{
			ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
		})
		info.ParamOverride = map[string]interface{}{
			"operations": []interface{}{map[string]interface{}{
				"mode":  "set",
				"path":  "instructions",
				"value": "compact override",
			}},
		}

		body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

		require.Nil(t, apiErr)
		bodyBytes, err := io.ReadAll(body)
		require.NoError(t, err)
		require.NotEqual(t, rawBody, bodyBytes)
		require.Contains(t, string(bodyBytes), `"instructions":"compact override"`)
		require.Contains(t, string(bodyBytes), `"context":"all_turns"`)
	})
}

func TestBuildOpenAIResponsesCompactRequestBodyConvertsMalformedOperationsOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":"compact","reasoning":{"context":"all_turns"}}`)
	c, request := setupResponsesCompactRequestBodyTest(t, rawBody)
	info := newResponsesCompactPassThroughInfo(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})
	info.ParamOverride = map[string]interface{}{
		"operations": []interface{}{"not an operation"},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NotEqual(t, rawBody, bodyBytes)
	var bodyMap map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(bodyBytes, &bodyMap))
	var operations []string
	require.NoError(t, common.Unmarshal(bodyMap["operations"], &operations))
	require.Equal(t, []string{"not an operation"}, operations)
	var reasoning map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(bodyMap["reasoning"], &reasoning))
	require.JSONEq(t, `"all_turns"`, string(reasoning["context"]))
}

func TestBuildOpenAIResponsesCompactRequestBodyNormalizesInvalidItemIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{
  "model":"gpt-5.6-sol",
  "input":[
    {"type":"function_call","id":"legacy_item","call_id":"call_keep","name":"exec","arguments":"{}"},
    {"type":"message","id":"msg_valid","role":"user","content":[{"type":"input_text","text":"compact"}]}
  ],
  "reasoning":{"context":"all_turns","future_reasoning_option":{"enabled":true}},
  "stream_options":{"reasoning_summary_delivery":"sequential_cutoff"},
  "future_compact_option":{"enabled":true}
}`)
	c, request := setupResponsesCompactRequestBodyTest(t, rawBody)
	request.Model = "provider/gpt-5.6-sol"
	info := newResponsesCompactPassThroughInfo(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NotEqual(t, rawBody, bodyBytes)
	var bodyMap map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(bodyBytes, &bodyMap))
	require.JSONEq(t, `{"enabled":true}`, string(bodyMap["future_compact_option"]))
	var streamOptions map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(bodyMap["stream_options"], &streamOptions))
	require.JSONEq(t, `"sequential_cutoff"`, string(streamOptions["reasoning_summary_delivery"]))
	var input []map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(bodyMap["input"], &input))
	require.Len(t, input, 2)
	_, hasInvalidID := input[0]["id"]
	require.False(t, hasInvalidID)
	var callID string
	require.NoError(t, common.Unmarshal(input[0]["call_id"], &callID))
	require.Equal(t, "call_keep", callID)
	var validID string
	require.NoError(t, common.Unmarshal(input[1]["id"], &validID))
	require.Equal(t, "msg_valid", validID)
	require.Equal(t, "provider/gpt-5.6-sol", converted.Model)
	require.Equal(t, 1, common.GetContextKeyInt(c, constant.ContextKeyResponsesInputItemIDsRemoved))
}
