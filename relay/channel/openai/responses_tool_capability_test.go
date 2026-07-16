package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestObserveResponsesToolCapabilityOutputMarksNativeToolKinds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	observeResponsesToolCapabilityOutput(c, &dto.ResponsesOutput{
		Type:      "custom_tool_call",
		Namespace: "mcp__tools",
	})
	observeResponsesToolCapabilityOutput(c, &dto.ResponsesOutput{
		Type: "tool_search_call",
	})

	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesObservedCustomToolCall))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesObservedNamespaceToolCall))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesObservedToolSearchCall))
}

func TestObserveResponsesToolCapabilityOutputMarksNamespacedFunctionCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	observeResponsesToolCapabilityOutput(c, &dto.ResponsesOutput{
		Type:      "function_call",
		Namespace: "mcp__tools",
	})

	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesObservedCustomToolCall))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesObservedNamespaceToolCall))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesObservedToolSearchCall))
}
