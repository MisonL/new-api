package openai

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiSyntheticResponsesCompactionHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var responsesResp dto.OpenAIResponsesResponse
	if err := common.Unmarshal(responseBody, &responsesResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResp.GetOpenAIError(); responsesCompactOpenAIErrorHasContent(oaiError) {
		return nil, types.WithOpenAIError(*oaiError, responsesCompactOpenAIErrorStatus(resp.StatusCode, oaiError))
	}

	model := ""
	if info != nil {
		model = info.OriginModelName
	}
	if model == "" {
		model = responsesResp.Model
	}
	if model == "" {
		return nil, types.NewOpenAIError(errors.New("model name is required"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	}
	compactResp, usage, err := service.BuildSyntheticCompactResponse(relaycommon.GinRequestContext(c), syntheticCompactScopeFromRelayInfo(info), model, responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	body, err := common.Marshal(compactResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	service.IOCopyBytesGracefully(c, resp, body)
	return usage, nil
}
