package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const (
	syntheticCompactEmbeddedMediaOmission      = "[embedded media omitted from synthetic compact summary]"
	syntheticCompactLargeToolPayloadOmission   = "[tool payload omitted from synthetic compact summary: payload is too large]"
	syntheticCompactLargeVisibleFieldOmission  = "[visible field omitted from synthetic compact summary: field is too large]"
	syntheticCompactLargeInputItemOmission     = "[input item omitted from synthetic compact summary: item is too large]"
	syntheticCompactToolPayloadMaxBytes        = 16 * 1024
	syntheticCompactVisibleFieldMaxBytes       = 1024 * 1024
	syntheticCompactVisibleInputItemMaxBytes   = 1024 * 1024
	syntheticCompactVisibleInputBufferMaxBytes = syntheticCompactVisibleTextMax * 2
	syntheticCompactDataURIScanMaxBytes        = syntheticCompactVisibleFieldMaxBytes
	syntheticCompactDataURIHeaderMaxBytes      = 512
	syntheticCompactJSONNestingDepthMax        = 64
	syntheticCompactMetadataFieldMaxBytes      = 16 * 1024
)

func syntheticCompactMarkers(input common.RawMessage) []string {
	if common.GetJsonType(input) != "array" || !bytes.Contains(input, []byte(syntheticCompactMarkerPrefix)) {
		return nil
	}
	var items []common.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		return nil
	}
	markers := make([]string, 0, 1)
	for _, rawItem := range items {
		item, ok := responsesInputObject(rawItem)
		if !ok {
			continue
		}
		if !relaycommon.IsResponsesCompactionItemType(rawStringField(item["type"])) {
			continue
		}
		if marker := rawStringField(item["encrypted_content"]); marker != "" {
			markers = append(markers, marker)
		}
	}
	return markers
}

func HasRemoteResponsesCompactionInput(ctx context.Context, req dto.OpenAIResponsesRequest) (bool, error) {
	if common.GetJsonType(req.Input) != "array" {
		return false, nil
	}
	decoder := common.NewJsonDecoderUseNumber(bytes.NewReader(req.Input))
	token, err := decoder.Token()
	if err != nil {
		return false, nil
	}
	delimiter, ok := token.(common.JsonDelim)
	if !ok || delimiter != '[' {
		return false, nil
	}

	hasRemote := false
	for decoder.More() {
		var item struct {
			Type             string            `json:"type"`
			EncryptedContent common.RawMessage `json:"encrypted_content"`
		}
		if err := common.DecodeJsonFromDecoder(decoder, &item); err != nil {
			return false, nil
		}
		itemType := strings.TrimSpace(item.Type)
		if !relaycommon.IsResponsesCompactionItemType(itemType) {
			continue
		}
		trimmedEncryptedContent := bytes.TrimSpace(item.EncryptedContent)
		encryptedContentType := common.GetJsonType(trimmedEncryptedContent)
		if len(trimmedEncryptedContent) > 0 && encryptedContentType != "null" && encryptedContentType != "string" {
			hasRemote = true
			continue
		}
		marker := rawStringField(item.EncryptedContent)
		if marker == "" {
			if itemType == dto.ResponsesOutputTypeContextCompaction {
				continue
			}
			return false, ErrResponsesCompactionContentRequired
		}
		if _, ok, err := syntheticCompactIDFromMarker(ctx, marker); err != nil {
			return false, err
		} else if !ok {
			hasRemote = true
		}
	}
	if _, err := decoder.Token(); err != nil {
		return false, nil
	}
	var extra any
	if err := common.DecodeJsonFromDecoder(decoder, &extra); err != io.EOF {
		return false, nil
	}
	return hasRemote, nil
}

func removeSyntheticCompactMarkers(ctx context.Context, input common.RawMessage) (common.RawMessage, error) {
	if common.GetJsonType(input) != "array" || !bytes.Contains(input, []byte(syntheticCompactMarkerPrefix)) {
		return input, nil
	}
	var items []common.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		// Malformed input stays opaque; callers only strip markers from parseable arrays.
		return input, nil
	}
	cleaned := make([]common.RawMessage, 0, len(items))
	for _, rawItem := range items {
		item, ok := responsesInputObject(rawItem)
		if !ok {
			cleaned = append(cleaned, rawItem)
			continue
		}
		if relaycommon.IsResponsesCompactionItemType(rawStringField(item["type"])) {
			if marker := rawStringField(item["encrypted_content"]); marker != "" {
				if _, ok, err := syntheticCompactIDFromMarker(ctx, marker); err != nil {
					return nil, err
				} else if ok {
					continue
				}
			}
		}
		cleaned = append(cleaned, rawItem)
	}
	data, err := common.Marshal(cleaned)
	if err != nil {
		return nil, fmt.Errorf("marshal synthetic compact marker-cleaned input: %w", err)
	}
	return data, nil
}

func normalizeResponsesInputItems(input common.RawMessage) []common.RawMessage {
	switch common.GetJsonType(input) {
	case "array":
		var items []common.RawMessage
		if err := common.Unmarshal(input, &items); err != nil {
			return nil
		}
		normalized := make([]common.RawMessage, 0, len(items))
		for _, rawItem := range items {
			if normalizedItem, ok := normalizeResponsesInputItem(rawItem); ok {
				normalized = append(normalized, normalizedItem)
			}
		}
		return normalized
	case "string":
		text := strings.TrimSpace(rawStringField(input))
		if text != "" {
			item, err := responseMessageInput("user", text)
			if err == nil {
				return []common.RawMessage{item}
			}
		}
	}
	return nil
}

func normalizeResponsesInputItem(rawItem common.RawMessage) (common.RawMessage, bool) {
	switch common.GetJsonType(rawItem) {
	case "object":
		return rawItem, true
	case "string":
		text := strings.TrimSpace(visibleResponsesField(rawItem))
		if text == "" {
			return nil, false
		}
		item, err := responseMessageInput("user", text)
		if err != nil {
			return nil, false
		}
		return item, true
	default:
		return nil, false
	}
}

func visibleResponsesInputParts(input common.RawMessage) []string {
	return visibleResponsesInputPartsWithLimit(input, syntheticCompactVisibleTextMax)
}

func visibleResponsesInputPartsWithLimit(input common.RawMessage, maxBytes int) []string {
	switch common.GetJsonType(input) {
	case "string":
		text := visibleResponsesField(input)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		limited := limitSyntheticCompactVisibleParts([]string{"[input] " + strings.TrimSpace(text)}, maxBytes)
		if limited == "" {
			return nil
		}
		return []string{limited}
	case "array":
		return visibleResponsesInputArrayPartsWithLimit(input, maxBytes)
	default:
		return nil
	}
}

func visibleResponsesInputArrayParts(input common.RawMessage) []string {
	return visibleResponsesInputArrayPartsWithLimit(input, syntheticCompactVisibleTextMax)
}

func visibleResponsesInputArrayPartsWithLimit(input common.RawMessage, maxBytes int) []string {
	tail := newVisibleResponsesInputTail(maxBytes)
	if !visitResponsesJSONArray(input, func(rawItem common.RawMessage) bool {
		tail.append(rawVisibleResponsesInputItem(rawItem))
		return true
	}) {
		return nil
	}
	return tail.parts()
}

type visibleResponsesInputTail struct {
	maxBytes  int
	data      []byte
	truncated bool
}

func newVisibleResponsesInputTail(maxBytes int) *visibleResponsesInputTail {
	return &visibleResponsesInputTail{maxBytes: maxBytes}
}

func (tail *visibleResponsesInputTail) append(text string) {
	if tail == nil || strings.TrimSpace(text) == "" {
		return
	}
	if len(tail.data) > 0 {
		tail.data = append(tail.data, '\n')
	}
	tail.data = append(tail.data, text...)
	if tail.maxBytes <= 0 || len(tail.data) <= syntheticCompactVisibleInputBufferMaxBytes {
		return
	}
	tail.keepLatest(tail.maxBytes)
}

func (tail *visibleResponsesInputTail) keepLatest(maxBytes int) {
	if tail == nil || maxBytes <= 0 || len(tail.data) <= maxBytes {
		return
	}
	trimmed := trimToRuneBoundary(string(tail.data[len(tail.data)-maxBytes:]))
	tail.data = append(tail.data[:0], trimmed...)
	tail.truncated = true
}

func (tail *visibleResponsesInputTail) parts() []string {
	if tail == nil {
		return nil
	}
	tail.keepLatest(tail.maxBytes)
	text := strings.TrimSpace(string(tail.data))
	if text == "" {
		return nil
	}
	if tail.truncated {
		text = "[truncated earlier visible input]\n" + text
	}
	return []string{text}
}

func rawVisibleResponsesInputItem(rawItem common.RawMessage) string {
	switch common.GetJsonType(rawItem) {
	case "string":
		text := strings.TrimSpace(visibleResponsesField(rawItem))
		if text == "" {
			return ""
		}
		return "[input] " + text
	case "object":
		if len(bytes.TrimSpace(rawItem)) > syntheticCompactVisibleInputItemMaxBytes {
			return visibleResponsesLargeInputItem(rawItem)
		}
		item, ok := responsesInputObject(rawItem)
		if !ok || relaycommon.IsResponsesCompactionItemType(rawStringField(item["type"])) {
			return ""
		}
		text := visibleResponsesItemText(item)
		if text == "" {
			return ""
		}
		label := rawStringField(item["role"])
		if label == "" {
			label = rawStringField(item["type"])
		}
		if label == "" {
			label = "input"
		}
		return fmt.Sprintf("[%s] %s", label, text)
	default:
		return ""
	}
}

func visibleResponsesLargeInputItem(rawItem common.RawMessage) string {
	fields, ok := visibleResponsesFieldsFromRaw(rawItem)
	if !ok {
		return syntheticCompactLargeInputItemOmission
	}
	itemType := visibleResponsesMetadataString(fields.Type)
	if relaycommon.IsResponsesCompactionItemType(itemType) {
		return ""
	}
	if text := visibleResponsesItemTextFromFields(fields); text != "" {
		label := visibleResponsesMetadataString(fields.Role)
		if label == "" {
			label = itemType
		}
		if label == "" {
			label = "input"
		}
		return fmt.Sprintf("[%s] %s", label, text)
	}
	label := visibleResponsesMetadataString(fields.Role)
	if label == "" {
		label = itemType
	}
	if label == "" {
		label = "input"
	}
	omission := syntheticCompactLargeInputItemOmission
	if hasEmbeddedMediaDataURI(rawItem) {
		omission = syntheticCompactEmbeddedMediaOmission
	}
	return fmt.Sprintf("[%s] %s", label, omission)
}

func responsesInputObject(rawItem common.RawMessage) (map[string]common.RawMessage, bool) {
	if common.GetJsonType(rawItem) != "object" {
		return nil, false
	}
	var item map[string]common.RawMessage
	if err := common.Unmarshal(rawItem, &item); err != nil {
		return nil, false
	}
	return item, true
}

func visibleResponsesItemText(item map[string]common.RawMessage) string {
	return visibleResponsesItemTextFromFields(visibleResponsesFields{
		Type:      item["type"],
		Role:      item["role"],
		Name:      item["name"],
		CallID:    item["call_id"],
		Text:      item["text"],
		Output:    item["output"],
		Input:     item["input"],
		Arguments: item["arguments"],
		Content:   item["content"],
	})
}

type visibleResponsesFields struct {
	Type      common.RawMessage
	Role      common.RawMessage
	Name      common.RawMessage
	CallID    common.RawMessage
	Text      common.RawMessage
	Output    common.RawMessage
	Input     common.RawMessage
	Arguments common.RawMessage
	Content   common.RawMessage
}

func visibleResponsesFieldsFromRaw(raw common.RawMessage) (visibleResponsesFields, bool) {
	var fields visibleResponsesFields
	ok := visitResponsesJSONObject(raw, func(key string, value common.RawMessage) bool {
		switch key {
		case "type":
			fields.Type = value
		case "role":
			fields.Role = value
		case "name":
			fields.Name = value
		case "call_id":
			fields.CallID = value
		case "text":
			fields.Text = value
		case "output":
			fields.Output = value
		case "input":
			fields.Input = value
		case "arguments":
			fields.Arguments = value
		case "content":
			fields.Content = value
		}
		return true
	})
	return fields, ok
}

func visibleResponsesItemTextFromFields(fields visibleResponsesFields) string {
	if text := visibleResponsesToolItemTextFromFields(fields); text != "" {
		return text
	}
	if isResponsesToolLikeType(visibleResponsesMetadataString(fields.Type)) {
		return ""
	}
	if text := visibleResponsesField(fields.Text); text != "" {
		return strings.TrimSpace(text)
	}
	if output := visibleResponsesField(fields.Output); output != "" {
		return strings.TrimSpace(output)
	}
	if input := visibleResponsesField(fields.Input); input != "" {
		return strings.TrimSpace(input)
	}
	if arguments := visibleResponsesField(fields.Arguments); arguments != "" {
		return arguments
	}
	switch common.GetJsonType(fields.Content) {
	case "string":
		return strings.TrimSpace(visibleResponsesField(fields.Content))
	case "array":
		return visibleResponsesContentText(fields.Content)
	default:
		return ""
	}
}

func visibleResponsesContentText(content common.RawMessage) string {
	tail := newVisibleResponsesInputTail(syntheticCompactVisibleTextMax)
	partCount := 0
	if !visitResponsesJSONArray(content, func(rawPart common.RawMessage) bool {
		fields, ok := visibleResponsesFieldsFromRaw(rawPart)
		if !ok {
			return true
		}
		partCount++
		if partCount > syntheticCompactVisibleContentPartsMax {
			return true
		}
		tail.append(visibleResponsesContentPartTextFromFields(rawPart, fields))
		return true
	}) {
		return ""
	}
	if partCount > syntheticCompactVisibleContentPartsMax {
		common.SysLog(fmt.Sprintf("synthetic compact visible content parts truncated: original=%d max=%d", partCount, syntheticCompactVisibleContentPartsMax))
	}
	parts := tail.parts()
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func visibleResponsesContentPartTextFromFields(rawPart common.RawMessage, fields visibleResponsesFields) string {
	partType := visibleResponsesMetadataString(fields.Type)
	switch partType {
	case "input_text", "output_text", "text":
		if text := strings.TrimSpace(visibleResponsesField(fields.Text)); text != "" {
			return text
		}
	case "input_image", "input_file":
		if hasEmbeddedMediaDataURI(rawPart) {
			return syntheticCompactEmbeddedMediaOmission
		}
	case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output":
		return visibleResponsesToolItemTextFromFields(fields)
	}
	if len(bytes.TrimSpace(rawPart)) > syntheticCompactVisibleInputItemMaxBytes {
		if hasEmbeddedMediaDataURI(rawPart) {
			return syntheticCompactEmbeddedMediaOmission
		}
		return syntheticCompactLargeInputItemOmission
	}
	return ""
}

func visibleResponsesToolItemText(item map[string]common.RawMessage) string {
	return visibleResponsesToolItemTextFromFields(visibleResponsesFields{
		Type:      item["type"],
		Name:      item["name"],
		CallID:    item["call_id"],
		Output:    item["output"],
		Input:     item["input"],
		Arguments: item["arguments"],
	})
}

func visibleResponsesToolItemTextFromFields(fields visibleResponsesFields) string {
	itemType := visibleResponsesMetadataString(fields.Type)
	if !isKnownResponsesToolItemType(itemType) {
		return ""
	}
	metadata := make([]string, 0, 2)
	if name := visibleResponsesMetadataString(fields.Name); name != "" {
		metadata = append(metadata, "name="+name)
	}
	if callID := visibleResponsesMetadataString(fields.CallID); callID != "" {
		metadata = append(metadata, "call_id="+callID)
	}
	payloads := make([]string, 0, 3)
	for _, field := range []struct {
		name  string
		value common.RawMessage
	}{
		{name: "output", value: fields.Output},
		{name: "input", value: fields.Input},
		{name: "arguments", value: fields.Arguments},
	} {
		if value := visibleResponsesToolPayloadField(field.value); value != "" {
			payloads = append(payloads, field.name+"="+value)
		}
	}
	parts := append(metadata, payloads...)
	return strings.Join(parts, "\n")
}

func isKnownResponsesToolItemType(itemType string) bool {
	switch itemType {
	case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output":
		return true
	default:
		return false
	}
}

func isResponsesToolLikeType(itemType string) bool {
	return strings.HasSuffix(itemType, "_call") || strings.HasSuffix(itemType, "_call_output")
}

func visibleResponsesMetadataString(raw common.RawMessage) string {
	if len(bytes.TrimSpace(raw)) > syntheticCompactMetadataFieldMaxBytes {
		return ""
	}
	return strings.TrimSpace(rawStringField(raw))
}

func visibleResponsesField(raw common.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if hasEmbeddedMediaDataURI(trimmed) {
		return syntheticCompactEmbeddedMediaOmission
	}
	if len(trimmed) > syntheticCompactVisibleFieldMaxBytes {
		return syntheticCompactLargeVisibleFieldOmission
	}
	return rawVisibleResponsesField(trimmed)
}

func visibleResponsesToolPayloadField(raw common.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if hasEmbeddedMediaDataURI(trimmed) {
		return syntheticCompactEmbeddedMediaOmission
	}
	if len(trimmed) > syntheticCompactToolPayloadMaxBytes {
		return syntheticCompactLargeToolPayloadOmission
	}
	return rawVisibleResponsesField(trimmed)
}

func hasEmbeddedMediaDataURI(raw []byte) bool {
	const dataURIPrefix = "data:"
	const dataURIPrefixLen = len(dataURIPrefix)
	scanLimit := syntheticCompactDataURIScanMaxBytes
	if len(raw) > scanLimit {
		// Keep enough overlap to complete a prefix that starts immediately before
		// the bounded scan window. The header bytes are included as well, but a
		// candidate whose start is at or beyond scanLimit remains out of scope.
		scanLimit += syntheticCompactDataURIHeaderMaxBytes
		if scanLimit > len(raw) {
			scanLimit = len(raw)
		}
		raw = raw[:scanLimit]
	}
	for offset := 0; offset < len(raw); {
		relativeStart := bytes.Index(raw[offset:], []byte(dataURIPrefix))
		if relativeStart < 0 {
			return false
		}
		start := offset + relativeStart
		if start >= syntheticCompactDataURIScanMaxBytes {
			return false
		}
		headerStart := start + len(dataURIPrefix)
		headerEnd := headerStart + syntheticCompactDataURIHeaderMaxBytes
		if headerEnd > len(raw) {
			headerEnd = len(raw)
		}
		header := raw[headerStart:headerEnd]
		if separator := bytes.Index(header, []byte(";base64,")); separator > 0 && bytes.IndexByte(header[:separator], '/') > 0 {
			return true
		}
		// Continue at the end of this candidate's bounded header unless another
		// candidate starts inside the same window. This keeps adversarial runs of
		// `data:` prefixes linear while still checking every possible URI start.
		nextSearch := start + dataURIPrefixLen
		if nextSearch >= headerEnd {
			offset = nextSearch
			continue
		}
		if nextRelative := bytes.Index(raw[nextSearch:headerEnd], []byte(dataURIPrefix)); nextRelative >= 0 {
			offset = nextSearch + nextRelative
			continue
		}
		offset = headerEnd
	}
	return false
}

func rawVisibleResponsesField(raw common.RawMessage) string {
	if value := rawStringField(raw); value != "" {
		return strings.TrimSpace(value)
	}
	if value := strings.TrimSpace(string(raw)); value != "" && value != "null" {
		return value
	}
	return ""
}

func rawStringField(raw common.RawMessage) string {
	if len(raw) == 0 || common.GetJsonType(raw) != "string" {
		return ""
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}
