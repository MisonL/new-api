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

type opaqueReplayResponseErrorAdaptor struct {
	retryEncryptedContextAdaptor
	err *types.NewAPIError
}

func (a *opaqueReplayResponseErrorAdaptor) DoResponse(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	return nil, a.err
}

type opaqueReplayStreamingStorage struct {
	*bytes.Reader
	size       int64
	bytesCalls int
}

func newOpaqueReplayStreamingStorage(body []byte) *opaqueReplayStreamingStorage {
	return &opaqueReplayStreamingStorage{
		Reader: bytes.NewReader(body),
		size:   int64(len(body)),
	}
}

func (s *opaqueReplayStreamingStorage) Close() error {
	return nil
}

func (s *opaqueReplayStreamingStorage) Bytes() ([]byte, error) {
	s.bytesCalls++
	return nil, errors.New("opaque replay body must not be materialized")
}

func (s *opaqueReplayStreamingStorage) Size() int64 {
	return s.size
}

func (s *opaqueReplayStreamingStorage) IsDisk() bool {
	return true
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

func TestExecuteOpenAIResponsesRequestKeepsForwardedOpaqueReplayActionWhenResponseHandlingFails(t *testing.T) {
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
	adaptor := &opaqueReplayResponseErrorAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{successResponsesHTTPResponse()},
		},
		err: types.NewError(errors.New("upstream response handling failed"), types.ErrorCodeBadResponse),
	}

	usage, apiErr := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionForwardedNative, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestExecuteOpaqueOpenAIResponsesRequestForwardsFailuresExactlyOnceWithoutMaterializingBody(t *testing.T) {
	statuses := []int{
		http.StatusRequestEntityTooLarge,
		http.StatusTooManyRequests,
		http.StatusBadGateway,
	}
	rawBody := []byte(`{
  "model":"gpt-5.6-sol",
  "input":[{"type":"compaction","encrypted_content":{"version":2,"ciphertext":"opaque"}}]
}`)

	for _, statusCode := range statuses {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			storage := newOpaqueReplayStreamingStorage(rawBody)
			c.Set(common.KeyBodyStorage, storage)
			modelLocation, err := common.LocateTopLevelJSONString(storage, "model")
			require.NoError(t, err)
			request := dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
				Model:                     "gpt-5.6-sol",
				ModelPresent:              true,
				ModelLocation:             modelLocation,
				InputPresent:              true,
				InputKind:                 common.ResponsesJSONValueKindArray,
				HasOpaqueEncryptedContent: true,
				HasCompactionItem:         true,
				BodySize:                  storage.Size(),
			})
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponses,
				OriginModelName: "gpt-5.6-sol",
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:   206,
					ChannelType: constant.ChannelTypeOpenAI,
				},
			}
			adaptor := &retryEncryptedContextAdaptor{
				responses: []*http.Response{encryptedContextRetryResponse(statusCode)},
			}

			usage, apiErr := executeOpaqueOpenAIResponsesRequest(c, info, adaptor, request)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Len(t, adaptor.requestBodies, 1)
			require.Equal(t, rawBody, adaptor.requestBodies[0])
			require.Zero(t, storage.bytesCalls)
			require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
		})
	}
}

func TestExecuteOpaqueOpenAIResponsesRequestValidatesDirectInvocation(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
		Model:        "gpt-5.6-sol",
		ModelPresent: true,
		InputPresent: true,
		InputKind:    common.ResponsesJSONValueKindArray,
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{}

	usage, apiErr := executeOpaqueOpenAIResponsesRequest(c, info, adaptor, request)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Empty(t, adaptor.requestBodies)
}

func TestValidateOpaqueOpenAIResponsesReplayRequiresTopLevelModelMetadata(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
		Model:                     "gpt-5.6-sol",
		InputPresent:              true,
		InputKind:                 common.ResponsesJSONValueKindArray,
		HasOpaqueEncryptedContent: true,
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	apiErr := validateOpaqueOpenAIResponsesReplay(c, info, request)

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "model is required")
}

func TestValidateOpaqueOpenAIResponsesReplayRejectsCompactWithoutNativeSupport(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	request := dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
		Model:                     "gpt-5.6-sol",
		ModelPresent:              true,
		InputPresent:              true,
		InputKind:                 common.ResponsesJSONValueKindArray,
		HasOpaqueEncryptedContent: true,
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeSynthetic,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
			},
		},
	}

	apiErr := validateOpaqueOpenAIResponsesReplay(c, info, request)

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	require.ErrorIs(t, apiErr, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionRejectedUnsupported, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestExecuteOpaqueOpenAIResponsesRequestReusesPrevalidatedReplay(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	storage := newOpaqueReplayStreamingStorage(rawBody)
	c.Set(common.KeyBodyStorage, storage)
	modelLocation, err := common.LocateTopLevelJSONString(storage, "model")
	require.NoError(t, err)
	request := dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
		Model:                     "gpt-5.6-sol",
		ModelPresent:              true,
		ModelLocation:             modelLocation,
		InputPresent:              true,
		InputKind:                 common.ResponsesJSONValueKindArray,
		HasOpaqueEncryptedContent: true,
		BodySize:                  storage.Size(),
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	require.Nil(t, validateOpaqueOpenAIResponsesReplay(c, info, request))
	info.ChannelOtherSettings.StripCodexEncryptedContext = true
	adaptor := &retryEncryptedContextAdaptor{responses: []*http.Response{successResponsesHTTPResponse()}}

	usage, apiErr := executeOpaqueOpenAIResponsesRequest(c, info, adaptor, request)

	require.NotNil(t, usage)
	require.Nil(t, apiErr)
	require.Equal(t, []byte(rawBody), adaptor.requestBodies[0])
	require.Zero(t, storage.bytesCalls)
}

func TestResponsesHelperKeepsOpaqueReplaySelectedWhenModelMappingFails(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 206)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.6-sol")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, "{")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"reasoning","encrypted_content":"opaque"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`),
		},
	}

	apiErr := ResponsesHelper(c, info)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeChannelModelMappedError, apiErr.GetErrorCode())
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNative, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	require.Equal(t, "supported", common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
}

func TestResponsesHelperKeepsOpaqueDTOReplaySelectedWhenModelMappingFails(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 206)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.6-sol")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, "{")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		Request: dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
			Model:                     "gpt-5.6-sol",
			ModelPresent:              true,
			InputPresent:              true,
			InputKind:                 common.ResponsesJSONValueKindArray,
			HasOpaqueEncryptedContent: true,
		}),
	}

	apiErr := ResponsesHelper(c, info)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeChannelModelMappedError, apiErr.GetErrorCode())
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNative, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
	require.Equal(t, "supported", common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
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

func TestBuildOpaqueOpenAIResponsesRequestBodyStreamsMappedModelWithoutBodyBytes(t *testing.T) {
	rawBody := []byte(`{
  "future_before": { "preserve" : [1, 2, 3] },
  "model" : "client-gpt-5.6-sol",
  "input" : [{"type":"reasoning","encrypted_content":{"version":2,"ciphertext":"opaque"}}],
  "future_after" : {"nested":[true,false,null]}
}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	storage := newOpaqueReplayStreamingStorage(rawBody)
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{
		Model: "upstream-gpt-5.6-sol",
		Input: common.RawMessage(`[{"type":"reasoning","encrypted_content":{"version":2,"ciphertext":"opaque"}}]`),
	}
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
	require.Zero(t, storage.bytesCalls)
}

func TestBuildOpaqueOpenAIResponsesReplayBodyRejectsMissingMetadataBodySize(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	storage := newOpaqueReplayStreamingStorage(rawBody)
	c.Set(common.KeyBodyStorage, storage)
	modelLocation, err := common.LocateTopLevelJSONString(storage, "model")
	require.NoError(t, err)

	request := dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
		Model:                     "gpt-5.6-sol",
		ModelPresent:              true,
		ModelLocation:             modelLocation,
		InputPresent:              true,
		InputKind:                 common.ResponsesJSONValueKindArray,
		HasOpaqueEncryptedContent: true,
	})

	body, apiErr := buildOpaqueOpenAIResponsesReplayBody(c, nil, request)

	require.Nil(t, body)
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "body size mismatch")
	require.Zero(t, storage.bytesCalls)
}

func TestBuildOpaqueOpenAIResponsesReplayBodyPreservesOriginalBytesExceptMappedModel(t *testing.T) {
	rawBody := []byte(`{
  "future_before" : {"keep":true},
  "model" : "client-gpt-5.6-sol",
  "input" : [{"type":"reasoning","encrypted_content":"opaque"}],
  "future_after" : [1, 2, 3]
}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	storage := newOpaqueReplayStreamingStorage(rawBody)
	c.Set(common.KeyBodyStorage, storage)
	modelLocation, err := common.LocateTopLevelJSONString(storage, "model")
	require.NoError(t, err)
	request := dto.NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
		Model:                     "client-gpt-5.6-sol",
		ModelPresent:              true,
		ModelLocation:             modelLocation,
		InputPresent:              true,
		InputKind:                 common.ResponsesJSONValueKindArray,
		HasOpaqueEncryptedContent: true,
		BodySize:                  storage.Size(),
	})

	body, apiErr := buildOpaqueOpenAIResponsesReplayBody(c, nil, request)
	require.Nil(t, apiErr)
	original, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, rawBody, original)

	request.SetModelName("upstream-gpt-5.6-sol")
	body, apiErr = buildOpaqueOpenAIResponsesReplayBody(c, nil, request)
	require.Nil(t, apiErr)
	mapped, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t,
		bytes.Replace(rawBody, []byte(`"client-gpt-5.6-sol"`), []byte(`"upstream-gpt-5.6-sol"`), 1),
		mapped,
	)
	require.Zero(t, storage.bytesCalls)
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
