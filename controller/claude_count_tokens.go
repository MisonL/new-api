package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type claudeCountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

func CountClaudeTokens(c *gin.Context) {
	request, err := helper.GetAndValidateClaudeRequest(c)
	if err != nil {
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			writeClaudeCountTokensError(c, http.StatusRequestEntityTooLarge, err, types.ErrorCodeReadRequestBodyFailed)
			return
		}
		writeClaudeCountTokensError(c, http.StatusBadRequest, err, types.ErrorCodeInvalidRequest)
		return
	}
	if err := validateClaudeCountTokensModelAccess(c, request.Model); err != nil {
		writeClaudeCountTokensError(c, http.StatusForbidden, err, types.ErrorCodeAccessDenied)
		return
	}

	common.SetContextKey(c, constant.ContextKeyOriginalModel, request.Model)
	info, err := relaycommon.GenRelayInfo(c, types.RelayFormatClaude, request, nil)
	if err != nil {
		writeClaudeCountTokensError(c, http.StatusInternalServerError, err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	// Dedicated counting endpoints should work even when billing-time token counting is disabled.
	tokens, err := service.EstimateRequestTokenForAPI(c, request.GetTokenCountMeta(), info)
	if err != nil {
		writeClaudeCountTokensError(c, http.StatusInternalServerError, err, types.ErrorCodeCountTokenFailed)
		return
	}

	c.JSON(http.StatusOK, claudeCountTokensResponse{InputTokens: tokens})
}

func validateClaudeCountTokensModelAccess(c *gin.Context, modelName string) error {
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		return nil
	}
	rawLimits, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
	if !ok {
		return errors.New("token has no model access")
	}
	limits, ok := rawLimits.(map[string]bool)
	if !ok {
		return errors.New("token model access configuration is invalid")
	}
	matchName := ratio_setting.FormatMatchingModelName(modelName)
	if !limits[matchName] {
		return fmt.Errorf("token is not allowed to access model %s", modelName)
	}
	return nil
}

func writeClaudeCountTokensError(c *gin.Context, statusCode int, err error, errorCode types.ErrorCode) {
	apiErr := types.NewErrorWithStatusCode(err, errorCode, statusCode, types.ErrOptionWithSkipRetry())
	c.JSON(apiErr.StatusCode, gin.H{
		"type":  "error",
		"error": apiErr.ToClaudeError(),
	})
}
