package controller

import (
	"net/http"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryResponsesCompactTimeoutStatusCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name       string
		relayMode  int
		statusCode int
		want       bool
	}{
		{
			name:       "compact retries gateway timeout",
			relayMode:  relayconstant.RelayModeResponsesCompact,
			statusCode: http.StatusGatewayTimeout,
			want:       true,
		},
		{
			name:       "compact retries upstream 524 timeout",
			relayMode:  relayconstant.RelayModeResponsesCompact,
			statusCode: 524,
			want:       true,
		},
		{
			name:       "normal responses keep legacy 524 behavior",
			relayMode:  relayconstant.RelayModeResponses,
			statusCode: 524,
			want:       false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			c.Set("relay_mode", tc.relayMode)
			err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, tc.statusCode)

			require.Equal(t, tc.want, shouldRetry(c, err, 1))
		})
	}
}

func TestShouldRetryResponsesCompactTimeoutStillHonorsRetryBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("relay_mode", relayconstant.RelayModeResponsesCompact)
	err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, 524)

	require.False(t, shouldRetry(c, err, 0))
}
