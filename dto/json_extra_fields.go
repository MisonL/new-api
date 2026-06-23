package dto

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
)

func collectUnknownJSONFields(rawMap map[string]json.RawMessage, knownFields map[string]struct{}) map[string]json.RawMessage {
	if len(rawMap) == 0 {
		return nil
	}
	extra := make(map[string]json.RawMessage)
	for key, value := range rawMap {
		if _, ok := knownFields[key]; ok {
			continue
		}
		extra[key] = cloneRawMessage(value)
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func marshalWithExtraJSONFields(base []byte, extra map[string]json.RawMessage) ([]byte, error) {
	if len(extra) == 0 {
		return base, nil
	}
	baseMap := map[string]json.RawMessage{}
	if err := common.Unmarshal(base, &baseMap); err != nil {
		return nil, err
	}
	for key, value := range extra {
		if _, exists := baseMap[key]; exists {
			continue
		}
		baseMap[key] = cloneRawMessage(value)
	}
	return common.Marshal(baseMap)
}

func cloneRawMessageMap(source map[string]json.RawMessage) map[string]json.RawMessage {
	if len(source) == 0 {
		return nil
	}
	clone := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		clone[key] = cloneRawMessage(value)
	}
	return clone
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	clone := make([]byte, len(value))
	copy(clone, value)
	return clone
}
