package common

import "github.com/QuantumNous/new-api/dto"

func IsResponsesCompactionItemType(itemType string) bool {
	return itemType == dto.ResponsesOutputTypeCompaction ||
		itemType == dto.ResponsesOutputTypeCompactionSummary ||
		itemType == dto.ResponsesOutputTypeContextCompaction
}

func IsResponsesCompactionTriggerItemType(itemType string) bool {
	return itemType == dto.ResponsesInputTypeCompactionTrigger
}
