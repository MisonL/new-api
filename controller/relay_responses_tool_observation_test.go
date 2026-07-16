package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesToolCapabilityObservationFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyResponsesObservedCustomToolCall, true)
	common.SetContextKey(c, constant.ContextKeyResponsesObservedNamespaceToolCall, true)
	now := time.Unix(100, 0).UTC()

	observation, ok := responsesToolCapabilityObservationFromContext(c, now)

	require.True(t, ok)
	require.Equal(t, int64(100), observation.ObservedAt)
	require.True(t, observation.CustomToolCall)
	require.True(t, observation.NamespaceToolCall)
	require.False(t, observation.ToolSearchCall)
}

func TestResponsesToolCapabilityObservationFromContextRequiresEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	_, ok := responsesToolCapabilityObservationFromContext(c, time.Unix(100, 0).UTC())

	require.False(t, ok)
}
