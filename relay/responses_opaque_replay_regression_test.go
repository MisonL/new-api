package relay

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type opaqueReplayRequestErrorAdaptor struct {
	retryEncryptedContextAdaptor
	err error
}

func (a *opaqueReplayRequestErrorAdaptor) DoRequest(_ *gin.Context, _ *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	body, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	a.requestBodies = append(a.requestBodies, body)
	return nil, a.err
}

func TestExecuteOpenAIResponsesRequestForwardsNonStringOpaqueCompactionUnchanged(t *testing.T) {
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{"type":"compaction","encrypted_content":{"version":2},"future_state":[1,2,3]}],
		"future_top_level":{"enabled":true}
	}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
			},
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{successResponsesHTTPResponse()},
	}

	usage, apiErr := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionForwardedNative, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestExecuteOpenAIResponsesRequestRecordsOpaqueReplayAttemptBeforeTransportFailure(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
			},
		},
	}
	adaptor := &opaqueReplayRequestErrorAdaptor{err: errors.New("upstream transport failed")}

	usage, apiErr := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionAttemptedNative, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestExecuteOpenAIResponsesRequestRecordsUnverifiedOpaqueReplayLifecycle(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"}]}`)
	tests := []struct {
		name       string
		adaptor    func() relaychannel.Adaptor
		wantAction string
		wantError  bool
	}{
		{
			name: "response received",
			adaptor: func() relaychannel.Adaptor {
				return &retryEncryptedContextAdaptor{responses: []*http.Response{successResponsesHTTPResponse()}}
			},
			wantAction: relaycommon.ResponsesEncryptedReasoningReplayActionForwardedNativeUnverified,
		},
		{
			name: "transport failure",
			adaptor: func() relaychannel.Adaptor {
				return &opaqueReplayRequestErrorAdaptor{err: errors.New("upstream transport failed")}
			},
			wantAction: relaycommon.ResponsesEncryptedReasoningReplayActionAttemptedNativeUnverified,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
			storage, err := common.CreateBodyStorage(rawBody)
			require.NoError(t, err)
			t.Cleanup(func() { _ = storage.Close() })
			c.Set(common.KeyBodyStorage, storage)

			request := &dto.OpenAIResponsesRequest{}
			require.NoError(t, common.Unmarshal(rawBody, request))
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponses,
				OriginModelName: request.Model,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:   206,
					ChannelType: constant.ChannelTypeOpenAI,
				},
			}

			usage, apiErr := executeOpenAIResponsesRequest(c, info, tt.adaptor(), request, false)

			if tt.wantError {
				require.Nil(t, usage)
				require.NotNil(t, apiErr)
			} else {
				require.NotNil(t, usage)
				require.Nil(t, apiErr)
			}
			require.Equal(t, tt.wantAction, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
		})
	}
}

func TestBuildOpaqueOpenAIResponsesRequestBodyOnlyRewritesMappedModel(t *testing.T) {
	rawBody := []byte(`{
  "future_before": { "preserve" : [1, 2, 3] },
  "model" : "client-gpt-5.6-sol",
  "input" : [{"type":"reasoning","encrypted_content":{"version":2,"ciphertext":"opaque"}}],
  "future_after" : {"nested":[true,false,null]}
}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	request.Model = "upstream-gpt-5.6-sol"
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "client-gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &retryEncryptedContextAdaptor{}, request, false)

	require.Nil(t, apiErr)
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	want := bytes.Replace(rawBody, []byte(`"client-gpt-5.6-sol"`), []byte(`"upstream-gpt-5.6-sol"`), 1)
	require.Equal(t, want, got)
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNativeUnverified, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestBuildOpaqueOpenAIResponsesRequestBodyAppliesHeaderOnlyParamOverride(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Originator", "Codex CLI")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"mode":  "pass_headers",
						"value": []interface{}{"Originator"},
					},
				},
			},
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &retryEncryptedContextAdaptor{}, request, false)

	require.Nil(t, apiErr)
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, rawBody, got)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
}

func TestBuildOpaqueOpenAIResponsesRequestBodyRejectsBodyParamOverride(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"mode":  "set",
						"path":  "instructions",
						"value": "must not mutate opaque replay",
					},
				},
			},
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &retryEncryptedContextAdaptor{}, request, false)

	require.Nil(t, body)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeChannelParamOverrideInvalid, apiErr.GetErrorCode())
	require.Contains(t, apiErr.Error(), "opaque encrypted replay")
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNativeUnverified, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}
