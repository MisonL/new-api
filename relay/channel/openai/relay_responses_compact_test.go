package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var responsesCompactGlobalStateMu sync.Mutex

func TestOaiResponsesCompactionHandlerNormalizesHTTP200ErrorBody(t *testing.T) {
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

	usage, err := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Equal(t, "context_too_large", err.ToOpenAIError().Code)
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerPassesValidCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	body := `{
		"id":"resp_compact",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"compaction","encrypted_content":"opaque"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 15, usage.TotalTokens)
	require.JSONEq(t, body, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerRecordsNativeOpaqueState(t *testing.T) {
	responsesCompactGlobalStateMu.Lock()
	t.Cleanup(responsesCompactGlobalStateMu.Unlock)

	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	db, err := gorm.Open(sqlite.Open("file:responses_compact_native_opaque?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.SyntheticCompactStateRecord{}))
	model.DB = db

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	info := &relaycommon.RelayInfo{
		UserId:          7,
		TokenId:         8,
		TokenGroup:      "default",
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   90,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	body := `{
		"id":"resp_compact",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"compaction","encrypted_content":"opaque"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.Equal(t, 15, usage.TotalTokens)
	require.JSONEq(t, body, recorder.Body.String())
	require.Equal(t, model.SyntheticCompactStateKindNativeOpaque, common.GetContextKeyString(c, constant.ContextKeyResponsesCompactMarkerKind))
	require.Equal(t, "recorded", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactStateLookup))
	require.Equal(t, "strict", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactStateScopeResult))
	require.Equal(t, "native_opaque_recorded", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))

	var record model.SyntheticCompactStateRecord
	require.NoError(t, model.DB.Where("kind = ?", model.SyntheticCompactStateKindNativeOpaque).First(&record).Error)
	require.Equal(t, model.SyntheticCompactScopePolicyStrict, record.ScopePolicy)
	require.Equal(t, model.SyntheticCompactOwnerScope(7, 8, "default"), record.OwnerScope)
	require.Equal(t, "resp_compact", record.UpstreamResponseID)
	require.Equal(t, "gpt-5.5", record.ModelAtCreation)
	require.NotEmpty(t, record.StateHash)
}

func TestOaiResponsesCompactionHandlerNormalizesValidCompactionSummaryOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	body := `{
		"id":"resp_compact",
		"object":"response.compaction",
		"created_at":1710000000,
		"output":[
			{"type":"message","content":[{"type":"input_text","text":"summary"}]},
			{"type":"compaction_summary","encrypted_content":"opaque"}
		],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 15, usage.TotalTokens)
	require.JSONEq(t, `{
		"id":"resp_compact",
		"object":"response",
		"created_at":1710000000,
		"output":[
			{"type":"message","content":[{"type":"input_text","text":"summary"}]},
			{"type":"compaction","encrypted_content":"opaque"}
		],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerRejectsMalformedCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_bad_compact",
			"object":"response",
			"output":[{"type":"message","content":[{"type":"output_text","text":"not compact"}]}]
		}`)),
	}

	usage, err := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
	require.Contains(t, err.Error(), "malformed compact output")
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerRejectsInvalidJSONAsMalformedCompactOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`not json`)),
	}

	usage, err := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
	require.Contains(t, err.Error(), "malformed compact output")
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerRejectsNonStringEncryptedContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_bad_compact",
			"object":"response",
			"output":[{"type":"compaction","encrypted_content":{"opaque":true}}]
		}`)),
	}

	usage, err := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
	require.Contains(t, err.Error(), "compaction output has no encrypted content")
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerRejectsEmptyCompactionSummaryEncryptedContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_bad_compact",
			"object":"response",
			"output":[{"type":"compaction_summary","encrypted_content":""}]
		}`)),
	}

	usage, err := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
	require.Contains(t, err.Error(), "compaction output has no encrypted content")
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerRejectsMalformedCompactionSummaryEncryptedContent(t *testing.T) {
	cases := []struct {
		name string
		item string
	}{
		{
			name: "missing",
			item: `{"type":"compaction_summary"}`,
		},
		{
			name: "null",
			item: `{"type":"compaction_summary","encrypted_content":null}`,
		},
		{
			name: "non_string",
			item: `{"type":"compaction_summary","encrypted_content":{"opaque":true}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"id":"resp_bad_compact",
					"object":"response",
					"output":[` + tc.item + `]
				}`)),
			}

			usage, err := OaiResponsesCompactionHandler(c, nil, resp)

			require.Nil(t, usage)
			require.NotNil(t, err)
			require.Equal(t, http.StatusBadGateway, err.StatusCode)
			require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
			require.Contains(t, err.Error(), "compaction output has no encrypted content")
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestResponsesCompactionOutputEncryptedContentSkipsEmptyItem(t *testing.T) {
	output := common.RawMessage(`[
		{"type":"compaction","encrypted_content":"   "},
		{"type":"message","role":"assistant","content":[]},
		{"type":"compaction_summary","encrypted_content":"opaque-valid-token"}
	]`)

	require.Equal(t, "opaque-valid-token", responsesCompactionOutputEncryptedContent(output))
}

func TestResponsesCompactOpenAIErrorStatus(t *testing.T) {
	t.Parallel()

	require.Equal(t, http.StatusBadRequest, responsesCompactOpenAIErrorStatus(http.StatusOK, &types.OpenAIError{
		Message: "string_above_max_length for input item",
		Type:    "invalid_request_error",
	}))
	require.Equal(t, http.StatusBadGateway, responsesCompactOpenAIErrorStatus(http.StatusOK, &types.OpenAIError{
		Message: "provider returned malformed compact output",
		Type:    "server_error",
	}))
	require.Equal(t, http.StatusTooManyRequests, responsesCompactOpenAIErrorStatus(http.StatusTooManyRequests, &types.OpenAIError{
		Message: "rate limited",
		Type:    "rate_limit_error",
	}))
}
