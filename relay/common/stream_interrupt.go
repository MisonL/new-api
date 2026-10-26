package common

import (
	"io"
	"net/http"

	commonutils "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// RetryableUpstreamStreamInterruptedError turns an upstream transport
// interruption into a retryable error, but only while the downstream client
// has not committed to a partial answer.
//
// The prelude flag is set by stream handlers while only lifecycle frames
// (message_start / pings) have been forwarded; once the first content frame
// goes out they clear it, because replaying after that would duplicate content.
func RetryableUpstreamStreamInterruptedError(c *gin.Context, info *RelayInfo) *types.NewAPIError {
	if info == nil || info.StreamStatus == nil {
		return nil
	}
	if info.StreamStatus.EndReason != StreamEndReasonUpstreamInterrupted {
		return nil
	}
	if info.ReceivedResponseCount > 0 &&
		(c == nil || !commonutils.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe)) {
		return nil
	}
	err := info.StreamStatus.EndError
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return types.NewOpenAIError(err, types.ErrorCodeUpstreamTransportInterrupted, http.StatusBadGateway)
}
