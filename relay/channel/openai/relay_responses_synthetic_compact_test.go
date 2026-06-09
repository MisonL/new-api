package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiSyntheticResponsesCompactionHandlerReturnsSyntheticCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	body := `{
		"id":"resp_upstream",
		"object":"response",
		"created_at":1710000000,
		"model":"gpt-5",
		"output":[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Synthetic summary text."}]}
		],
		"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	info := &relaycommon.RelayInfo{
		UserId:          10,
		TokenId:         20,
		UsingGroup:      "default",
		OriginModelName: "gpt-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelId:   163,
		},
	}

	usage, err := OaiSyntheticResponsesCompactionHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 10, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)

	var compactResp dto.OpenAIResponsesCompactionResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &compactResp))
	require.NotEmpty(t, compactResp.ID)
	output := []map[string]interface{}{}
	require.NoError(t, common.Unmarshal(compactResp.Output, &output))
	require.Len(t, output, 1)
	require.Equal(t, "compaction", output[0]["type"])
	encryptedContent, ok := output[0]["encrypted_content"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(encryptedContent, "newapi.synthetic.compact:v2:"))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: compactResp.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	applied, ok, applyErr := service.ApplySyntheticCompactState(c.Request.Context(), service.SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}, req)
	require.NoError(t, applyErr)
	require.True(t, ok)
	require.Empty(t, applied.PreviousResponseID)
	require.Contains(t, string(applied.Input), "Synthetic summary text.")
}

func TestOaiSyntheticResponsesCompactionHandlerStoresOriginalModelWhenFallbackModelWasUsed(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	body := `{
		"id":"resp_upstream",
		"object":"response",
		"created_at":1710000000,
		"model":"gpt-5.4",
		"output":[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Fallback summary text."}]}
		],
		"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		UserId:          10,
		TokenId:         20,
		UsingGroup:      "default",
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         163,
			UpstreamModelName: "gpt-5.4",
		},
	}

	usage, err := OaiSyntheticResponsesCompactionHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 10, usage.PromptTokens)
	var compactResp dto.OpenAIResponsesCompactionResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &compactResp))
	applied, ok, applyErr := service.ApplySyntheticCompactState(c.Request.Context(), service.SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}, dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: compactResp.ID,
		Input:              common.RawMessage(`"continue"`),
	})
	require.NoError(t, applyErr)
	require.True(t, ok)
	require.Contains(t, string(applied.Input), "Fallback summary text.")
}

func TestOaiSyntheticResponsesCompactionHandlerReturnsUpstreamOpenAIError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"bad compact request"}}`)),
	}

	usage, err := OaiSyntheticResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
}

func TestOaiSyntheticResponsesCompactionHandlerNormalizesHTTP200ErrorBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"error": {
				"message": "Your input exceeds the context window of this model. Please adjust your input and try again.",
				"type": "invalid_request_error",
				"code": "context_too_large"
			}
		}`)),
	}

	usage, err := OaiSyntheticResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Equal(t, "context_too_large", err.ToOpenAIError().Code)
	require.Empty(t, recorder.Body.String())
}

func TestOaiSyntheticResponsesCompactionHandlerRejectsMalformedJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{`)),
	}

	usage, err := OaiSyntheticResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
}

func TestOaiSyntheticResponsesCompactionHandlerRequiresModel(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_upstream",
			"object":"response",
			"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"summary"}]}]
		}`)),
	}

	usage, err := OaiSyntheticResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeInvalidRequest, err.GetErrorCode())
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
}

func TestOaiSyntheticResponsesCompactionHandlerRejectsEmptySummary(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_upstream",
			"object":"response",
			"model":"gpt-5",
			"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"   "}]}]
		}`)),
	}

	usage, err := OaiSyntheticResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
}
