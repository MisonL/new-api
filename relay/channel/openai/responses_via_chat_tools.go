package openai

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
)

type chatToolCallState struct {
	outputIndex        int
	itemID             string
	callID             string
	toolType           string
	chatToolType       string
	name               string
	namespace          string
	arguments          strings.Builder
	input              strings.Builder
	argumentsPublished int
	customInputSeen    bool
	sentAdded          bool
}

func orderedChatToolCallStates(toolCalls map[int]*chatToolCallState) []*chatToolCallState {
	indices := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	states := make([]*chatToolCallState, 0, len(indices))
	for _, index := range indices {
		if state := toolCalls[index]; state != nil {
			states = append(states, state)
		}
	}
	return states
}

func applyResponsesChatToolIdentity(state *chatToolCallState, toolState *service.ResponsesChatCompatibilityToolState) {
	if state == nil || strings.TrimSpace(state.name) == "" {
		return
	}
	identity, ok := toolState.Resolve(state.name)
	if !ok {
		return
	}
	state.toolType = identity.Type
	state.name = identity.Name
	state.namespace = identity.Namespace
	if state.toolType == "tool_search" {
		state.itemID = strings.Replace(state.itemID, "fc_", "tsc_", 1)
	}
	if state.toolType == dto.CustomType {
		state.itemID = strings.Replace(state.itemID, "fc_", "ctc_", 1)
	}
}

func marshalResponsesCompatArguments(arguments string) (common.RawMessage, error) {
	return common.Marshal(arguments)
}

func responsesToolSearchArgumentsRaw(arguments string) (common.RawMessage, error) {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return nil, fmt.Errorf("tool_search arguments are required")
	}
	raw := common.RawMessage(trimmed)
	if !common.ValidJson(raw) || common.GetJsonType(raw) != "object" {
		return nil, fmt.Errorf("tool_search arguments must be a JSON object")
	}
	var payload map[string]any
	if err := common.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse tool_search arguments: %w", err)
	}
	if strings.TrimSpace(common.Interface2String(payload["query"])) == "" {
		return nil, fmt.Errorf("tool_search arguments require query")
	}
	return raw, nil
}

func parseChatCustomStreamDelta(raw []byte) (string, string, bool, error) {
	if len(raw) == 0 {
		return "", "", false, nil
	}
	var custom map[string]any
	if err := common.Unmarshal(raw, &custom); err != nil {
		return "", "", false, err
	}
	name := strings.TrimSpace(common.Interface2String(custom["name"]))
	rawInput, exists := custom["input"]
	if !exists {
		return name, "", false, nil
	}
	input, ok := rawInput.(string)
	if !ok {
		return "", "", false, fmt.Errorf("custom tool input must be a string")
	}
	return name, input, true, nil
}
