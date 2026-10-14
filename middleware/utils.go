package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func abortWithOpenAiMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
	codeStr := ""
	if len(code) > 0 {
		codeStr = string(code[0])
	}
	userId := c.GetInt("id")
	messageWithRequestID := common.MessageWithRequestId(message, c.GetString(common.RequestIdKey))
	// The type must come from the client-facing vocabulary (rate_limit_error,
	// authentication_error, ...), not the gateway-internal "new_api_error":
	// clients route retries on the standard names. The status is the primary
	// signal; the explicit code, when one is passed, breaks ties.
	errorType := types.StandardErrorTypeForStatus(statusCode)
	for _, oneCode := range code {
		errorType = types.StandardErrorTypeName(oneCode, statusCode)
		break
	}
	if service.ShouldWriteResponsesBootstrapStreamError(c) {
		helper.SetEventStreamHeaders(c)
		service.MarkResponsesBootstrapHeadersSent(c)
		err := helper.OpenAIErrorEvent(c, types.OpenAIError{
			Message: messageWithRequestID,
			Type:    errorType,
			Code:    codeStr,
		})
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | write bootstrap stream error failed: %s", userId, err.Error()))
		}
		c.Abort()
		logger.LogError(requestContext(c), fmt.Sprintf("user %d | %s", userId, message))
		return
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": messageWithRequestID,
			"type":    errorType,
			"code":    codeStr,
		},
	})
	c.Abort()
	logger.LogError(requestContext(c), fmt.Sprintf("user %d | %s", userId, message))
}

// requestContext returns the request's context, tolerating a context without a
// request (test harnesses build bare gin contexts): logging must never be the
// thing that panics an error path.
func requestContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}

// abortRateLimited rejects a request with 429 plus a Retry-After hint and a
// standard rate_limit_error type, so the client's backoff logic has both the
// timing and the type it expects.
func abortRateLimited(c *gin.Context, retryAfterSeconds int) {
	if retryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.Itoa(retryAfterSeconds))
	}
	abortWithOpenAiMessage(c, http.StatusTooManyRequests, "rate limit exceeded, please retry later")
}

func abortWithMidjourneyMessage(c *gin.Context, statusCode int, code int, description string) {
	c.JSON(statusCode, gin.H{
		"description": description,
		"type":        "new_api_error",
		"code":        code,
	})
	c.Abort()
	logger.LogError(c.Request.Context(), description)
}
