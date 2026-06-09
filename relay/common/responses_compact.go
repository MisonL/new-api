package common

func IsResponsesCompactionItemType(itemType string) bool {
	return itemType == "compaction" || itemType == "compaction_summary"
}
