package openai

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

func observeResponsesToolCapabilityOutput(c *gin.Context, output *dto.ResponsesOutput) {
	if c == nil || output == nil {
		return
	}
	itemType := strings.TrimSpace(output.Type)
	switch itemType {
	case "custom_tool_call":
		common.SetContextKey(c, constant.ContextKeyResponsesObservedCustomToolCall, true)
	case "tool_search_call":
		common.SetContextKey(c, constant.ContextKeyResponsesObservedToolSearchCall, true)
	}
	if strings.TrimSpace(output.Namespace) != "" &&
		(itemType == "function_call" || itemType == "custom_tool_call") {
		common.SetContextKey(c, constant.ContextKeyResponsesObservedNamespaceToolCall, true)
	}
}

func observeResponsesToolCapabilityOutputs(c *gin.Context, outputs []dto.ResponsesOutput) {
	for index := range outputs {
		observeResponsesToolCapabilityOutput(c, &outputs[index])
	}
}
