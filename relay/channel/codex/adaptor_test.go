package codex

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestConvertOpenAIResponsesRequestRestoresSyntheticCompactState(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		UserId:      10,
		TokenId:     20,
		UsingGroup:  "default",
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	scope := service.SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	compactResp, _, err := service.BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5.5", dto.OpenAIResponsesResponse{
		CreatedAt: 1710000000,
		Model:     "gpt-5.5",
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "Stored codex switch summary."},
				},
			},
		},
	})
	require.NoError(t, err)
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: compactResp.ID,
		Input:              common.RawMessage(`"continue"`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.NoError(t, err)
	convertedReq, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	require.Empty(t, convertedReq.PreviousResponseID)
	require.Contains(t, string(convertedReq.Input), "Stored codex switch summary.")
	require.JSONEq(t, `false`, string(convertedReq.Store))
}

func TestConvertOpenAIResponsesRequestPropagatesSyntheticCompactErrors(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		UserId:      10,
		TokenId:     20,
		UsingGroup:  "default",
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_newapi_synthcmp_missing",
		Input:              common.RawMessage(`"continue"`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)

	require.Nil(t, converted)
	require.ErrorIs(t, err, service.ErrSyntheticCompactStateNotFound)
	require.Equal(t, "missing_local_synthetic_state", common.GetContextKeyString(c, constant.ContextKeyResponsesPreviousIDAction))
}
