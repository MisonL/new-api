package common

import (
	"errors"
	"strings"
)

// The opaque replay scanner records only bounded structural facts from input and tools.
type responsesInputScanResult struct {
	hasOpaqueEncryptedContent             bool
	hasCompactionItem                     bool
	encryptedContentFingerprints          []string
	encryptedContentFingerprintsTruncated bool
	shape                                 responsesInputShape
}

type responsesInputShape struct {
	itemCount      int
	itemCountKnown bool
	itemTypes      map[string]int

	hasAdditionalTools              bool
	additionalToolCount             int
	additionalToolCountKnown        bool
	additionalToolTypes             map[string]int
	additionalCustomFormatTypes     map[string]int
	additionalCustomGrammarSyntaxes map[string]int

	hasCompactionTrigger bool
}

type responsesToolsShape struct {
	count                 int
	countKnown            bool
	toolTypes             map[string]int
	customFormatTypes     map[string]int
	customGrammarSyntaxes map[string]int
}

type responsesReasoningShape struct {
	present      bool
	detailsKnown bool
	hasContext   bool
	hasMode      bool
}

func (r *responsesInputScanResult) merge(other responsesInputScanResult) {
	if r == nil {
		return
	}
	r.hasOpaqueEncryptedContent = r.hasOpaqueEncryptedContent || other.hasOpaqueEncryptedContent
	r.hasCompactionItem = r.hasCompactionItem || other.hasCompactionItem
	r.encryptedContentFingerprintsTruncated = r.encryptedContentFingerprintsTruncated || other.encryptedContentFingerprintsTruncated
	for _, fingerprint := range other.encryptedContentFingerprints {
		if fingerprint == "" || containsString(r.encryptedContentFingerprints, fingerprint) {
			continue
		}
		if len(r.encryptedContentFingerprints) >= maxResponsesMetadataEncryptedContentFingerprints {
			r.encryptedContentFingerprintsTruncated = true
			continue
		}
		r.encryptedContentFingerprints = append(r.encryptedContentFingerprints, fingerprint)
	}
	r.shape.merge(other.shape)
}

func (s *jsonTopLevelScanner) scanResponsesInputValue(first byte, depth int, collectInputEncryptedContentFingerprint bool, directInputItem bool, collectInputShape bool) (responsesInputScanResult, error) {
	if depth > 1000 {
		return responsesInputScanResult{}, errors.New("JSON nesting depth exceeds 1000")
	}
	switch first {
	case '{':
		return s.scanResponsesInputObject(depth+1, collectInputEncryptedContentFingerprint, directInputItem, collectInputShape)
	case '[':
		return s.scanResponsesInputArray(depth+1, collectInputEncryptedContentFingerprint, directInputItem, collectInputShape)
	default:
		if err := s.skipValue(first, depth); err != nil {
			return responsesInputScanResult{}, err
		}
		return responsesInputScanResult{}, nil
	}
}

func (s *jsonTopLevelScanner) scanResponsesInputArray(depth int, collectInputEncryptedContentFingerprint bool, elementsAreDirectInputItems bool, collectInputShape bool) (responsesInputScanResult, error) {
	var result responsesInputScanResult
	next, err := s.nextNonWhitespace()
	if err != nil {
		return result, err
	}
	if next == ']' {
		return result, nil
	}
	for {
		childIsDirectInputItem := elementsAreDirectInputItems && next == '{'
		child, err := s.scanResponsesInputValue(
			next,
			depth,
			collectInputEncryptedContentFingerprint,
			childIsDirectInputItem,
			collectInputShape && childIsDirectInputItem,
		)
		if err != nil {
			return result, err
		}
		result.merge(child)
		next, err = s.nextNonWhitespace()
		if err != nil {
			return result, err
		}
		switch next {
		case ',':
			next, err = s.nextNonWhitespace()
			if err != nil {
				return result, err
			}
		case ']':
			return result, nil
		default:
			return result, errors.New("JSON array value is missing comma")
		}
	}
}

func (shape *responsesInputShape) merge(other responsesInputShape) {
	if shape == nil {
		return
	}
	if other.itemCountKnown {
		shape.itemCountKnown = true
		shape.itemCount += other.itemCount
	}
	mergeResponsesShapeCountMap(&shape.itemTypes, other.itemTypes)
	if other.hasAdditionalTools {
		if !shape.hasAdditionalTools {
			shape.additionalToolCountKnown = other.additionalToolCountKnown
		} else {
			shape.additionalToolCountKnown = shape.additionalToolCountKnown && other.additionalToolCountKnown
		}
		if other.additionalToolCountKnown {
			shape.additionalToolCount += other.additionalToolCount
		}
	}
	shape.hasAdditionalTools = shape.hasAdditionalTools || other.hasAdditionalTools
	mergeResponsesShapeCountMap(&shape.additionalToolTypes, other.additionalToolTypes)
	mergeResponsesShapeCountMap(&shape.additionalCustomFormatTypes, other.additionalCustomFormatTypes)
	mergeResponsesShapeCountMap(&shape.additionalCustomGrammarSyntaxes, other.additionalCustomGrammarSyntaxes)
	shape.hasCompactionTrigger = shape.hasCompactionTrigger || other.hasCompactionTrigger
}

func (metadata *ResponsesRequestShapeMetadata) setInputShape(shape responsesInputShape) {
	if metadata == nil {
		return
	}
	metadata.InputItemCount = shape.itemCount
	metadata.InputItemCountKnown = shape.itemCountKnown
	metadata.InputItemTypes = cloneResponsesShapeCountMap(shape.itemTypes)
	metadata.HasAdditionalTools = shape.hasAdditionalTools
	metadata.AdditionalToolCount = shape.additionalToolCount
	metadata.AdditionalToolCountKnown = shape.additionalToolCountKnown
	metadata.AdditionalToolTypes = cloneResponsesShapeCountMap(shape.additionalToolTypes)
	metadata.AdditionalCustomFormatTypes = cloneResponsesShapeCountMap(shape.additionalCustomFormatTypes)
	metadata.AdditionalCustomGrammarSyntaxes = cloneResponsesShapeCountMap(shape.additionalCustomGrammarSyntaxes)
	metadata.HasCompactionTrigger = shape.hasCompactionTrigger
}

func (metadata *ResponsesRequestShapeMetadata) setTopLevelToolShape(shape responsesToolsShape) {
	if metadata == nil {
		return
	}
	metadata.TopLevelToolCount = shape.count
	metadata.TopLevelToolCountKnown = shape.countKnown
	metadata.TopLevelToolTypes = cloneResponsesShapeCountMap(shape.toolTypes)
	metadata.TopLevelCustomFormatTypes = cloneResponsesShapeCountMap(shape.customFormatTypes)
	metadata.TopLevelCustomGrammarSyntaxes = cloneResponsesShapeCountMap(shape.customGrammarSyntaxes)
}

func (metadata *ResponsesRequestShapeMetadata) setReasoningShape(shape responsesReasoningShape) {
	if metadata == nil {
		return
	}
	metadata.HasReasoning = shape.present
	metadata.ReasoningDetailsKnown = shape.detailsKnown
	metadata.HasReasoningContext = shape.hasContext
	metadata.HasReasoningMode = shape.hasMode
}

func mergeResponsesShapeCountMap(target *map[string]int, source map[string]int) {
	if len(source) == 0 {
		return
	}
	if *target == nil {
		*target = make(map[string]int, len(source))
	}
	for key, value := range source {
		(*target)[key] += value
	}
}

func (s *jsonTopLevelScanner) scanResponsesInputObject(depth int, collectInputEncryptedContentFingerprint bool, directInputItem bool, collectInputShape bool) (responsesInputScanResult, error) {
	var result responsesInputScanResult
	next, err := s.nextNonWhitespace()
	if err != nil {
		return result, err
	}
	if next == '}' {
		return result, nil
	}

	itemType := ""
	var encryptedContent responsesEncryptedContentSummary
	var additionalTools responsesToolsShape
	for {
		if next != '"' {
			return result, errors.New("JSON object field name must be a string")
		}
		field, captured, err := s.readJSONString(maxTopLevelJSONStringCaptureBytes)
		if err != nil {
			return result, err
		}
		next, err = s.nextNonWhitespace()
		if err != nil {
			return result, err
		}
		if next != ':' {
			return result, errors.New("JSON object field is missing colon")
		}
		value, err := s.nextNonWhitespace()
		if err != nil {
			return result, err
		}

		switch {
		case captured && field == "type":
			valueString, valueCaptured, err := s.readResponsesInputOptionalString(value, depth)
			if err != nil {
				return result, err
			}
			if valueCaptured {
				itemType = valueString
			} else {
				itemType = ""
			}
		case captured && field == "encrypted_content":
			summary, err := s.readResponsesEncryptedContent(value, depth)
			if err != nil {
				return result, err
			}
			encryptedContent.merge(summary)
		case captured && field == "tools" && directInputItem:
			toolShape, err := s.scanResponsesToolsValue(value, depth+1)
			if err != nil {
				return result, err
			}
			additionalTools = toolShape
		case value == '{' || value == '[':
			child, err := s.scanResponsesInputValue(value, depth+1, collectInputEncryptedContentFingerprint, false, false)
			if err != nil {
				return result, err
			}
			result.merge(child)
		default:
			if err := s.skipValue(value, depth); err != nil {
				return result, err
			}
		}

		next, err = s.nextNonWhitespace()
		if err != nil {
			return result, err
		}
		switch next {
		case ',':
			next, err = s.nextNonWhitespace()
			if err != nil {
				return result, err
			}
		case '}':
			goto complete
		default:
			return result, errors.New("JSON object field is missing comma")
		}
	}

complete:
	itemType = strings.TrimSpace(itemType)
	if directInputItem && isResponsesMetadataCompactionItemType(itemType) {
		result.hasCompactionItem = true
	}
	if directInputItem && collectInputShape {
		result.shape.itemCountKnown = true
		result.shape.itemCount++
		mergeResponsesShapeCountMap(&result.shape.itemTypes, map[string]int{
			redactResponsesMetadataShapeType(itemType, responsesMetadataInputShapeTypes): 1,
		})
		if itemType == "additional_tools" {
			result.shape.hasAdditionalTools = true
			result.shape.additionalToolCountKnown = additionalTools.countKnown
			if additionalTools.countKnown {
				result.shape.additionalToolCount = additionalTools.count
			}
			mergeResponsesShapeCountMap(&result.shape.additionalToolTypes, additionalTools.toolTypes)
			mergeResponsesShapeCountMap(&result.shape.additionalCustomFormatTypes, additionalTools.customFormatTypes)
			mergeResponsesShapeCountMap(&result.shape.additionalCustomGrammarSyntaxes, additionalTools.customGrammarSyntaxes)
		}
		if itemType == "compaction_trigger" {
			result.shape.hasCompactionTrigger = true
		}
	}
	if encryptedContent.hasOpaqueContent {
		result.hasOpaqueEncryptedContent = true
		if collectInputEncryptedContentFingerprint && encryptedContent.fingerprint != "" {
			result.merge(responsesInputScanResult{encryptedContentFingerprints: []string{encryptedContent.fingerprint}})
		}
	}
	return result, nil
}

type responsesToolFormatShape struct {
	formatType  string
	grammarType string
}

func (s *jsonTopLevelScanner) scanResponsesToolsValue(first byte, depth int) (responsesToolsShape, error) {
	var shape responsesToolsShape
	if depth > 1000 {
		return shape, errors.New("JSON nesting depth exceeds 1000")
	}
	if first != '[' {
		if err := s.skipValue(first, depth); err != nil {
			return shape, err
		}
		return shape, nil
	}
	shape.countKnown = true
	next, err := s.nextNonWhitespace()
	if err != nil {
		return shape, err
	}
	if next == ']' {
		return shape, nil
	}
	for {
		toolType, format, err := s.scanResponsesToolValue(next, depth+1)
		if err != nil {
			return shape, err
		}
		shape.count++
		mergeResponsesShapeCountMap(&shape.toolTypes, map[string]int{toolType: 1})
		if toolType == "custom" {
			if format.formatType != "" {
				mergeResponsesShapeCountMap(&shape.customFormatTypes, map[string]int{format.formatType: 1})
			}
			if format.grammarType != "" {
				mergeResponsesShapeCountMap(&shape.customGrammarSyntaxes, map[string]int{format.grammarType: 1})
			}
		}
		next, err = s.nextNonWhitespace()
		if err != nil {
			return shape, err
		}
		switch next {
		case ',':
			next, err = s.nextNonWhitespace()
			if err != nil {
				return shape, err
			}
		case ']':
			return shape, nil
		default:
			return shape, errors.New("JSON array value is missing comma")
		}
	}
}

func (s *jsonTopLevelScanner) scanResponsesToolValue(first byte, depth int) (string, responsesToolFormatShape, error) {
	if depth > 1000 {
		return "", responsesToolFormatShape{}, errors.New("JSON nesting depth exceeds 1000")
	}
	if first == '"' {
		if _, _, err := s.readJSONString(0); err != nil {
			return "", responsesToolFormatShape{}, err
		}
		return "custom", responsesToolFormatShape{}, nil
	}
	if first != '{' {
		if err := s.skipValue(first, depth); err != nil {
			return "", responsesToolFormatShape{}, err
		}
		return "other", responsesToolFormatShape{}, nil
	}

	toolType := "other"
	var format responsesToolFormatShape
	next, err := s.nextNonWhitespace()
	if err != nil {
		return "", responsesToolFormatShape{}, err
	}
	if next == '}' {
		return toolType, format, nil
	}
	for {
		if next != '"' {
			return "", responsesToolFormatShape{}, errors.New("JSON object field name must be a string")
		}
		field, captured, err := s.readJSONString(maxTopLevelJSONStringCaptureBytes)
		if err != nil {
			return "", responsesToolFormatShape{}, err
		}
		next, err = s.nextNonWhitespace()
		if err != nil {
			return "", responsesToolFormatShape{}, err
		}
		if next != ':' {
			return "", responsesToolFormatShape{}, errors.New("JSON object field is missing colon")
		}
		value, err := s.nextNonWhitespace()
		if err != nil {
			return "", responsesToolFormatShape{}, err
		}

		switch {
		case captured && field == "type":
			valueString, valueCaptured, err := s.readResponsesInputOptionalString(value, depth)
			if err != nil {
				return "", responsesToolFormatShape{}, err
			}
			if valueCaptured {
				toolType = redactResponsesMetadataShapeType(strings.TrimSpace(valueString), responsesMetadataToolShapeTypes)
			} else {
				toolType = "other"
			}
		case captured && field == "format":
			format, err = s.scanResponsesToolFormatValue(value, depth+1)
			if err != nil {
				return "", responsesToolFormatShape{}, err
			}
		default:
			if err := s.skipValue(value, depth); err != nil {
				return "", responsesToolFormatShape{}, err
			}
		}

		next, err = s.nextNonWhitespace()
		if err != nil {
			return "", responsesToolFormatShape{}, err
		}
		switch next {
		case ',':
			next, err = s.nextNonWhitespace()
			if err != nil {
				return "", responsesToolFormatShape{}, err
			}
		case '}':
			return toolType, format, nil
		default:
			return "", responsesToolFormatShape{}, errors.New("JSON object field is missing comma")
		}
	}
}

func (s *jsonTopLevelScanner) scanResponsesToolFormatValue(first byte, depth int) (responsesToolFormatShape, error) {
	var shape responsesToolFormatShape
	if depth > 1000 {
		return shape, errors.New("JSON nesting depth exceeds 1000")
	}
	if first != '{' {
		if err := s.skipValue(first, depth); err != nil {
			return shape, err
		}
		return shape, nil
	}
	next, err := s.nextNonWhitespace()
	if err != nil {
		return shape, err
	}
	if next == '}' {
		return shape, nil
	}
	for {
		if next != '"' {
			return shape, errors.New("JSON object field name must be a string")
		}
		field, captured, err := s.readJSONString(maxTopLevelJSONStringCaptureBytes)
		if err != nil {
			return shape, err
		}
		next, err = s.nextNonWhitespace()
		if err != nil {
			return shape, err
		}
		if next != ':' {
			return shape, errors.New("JSON object field is missing colon")
		}
		value, err := s.nextNonWhitespace()
		if err != nil {
			return shape, err
		}

		switch {
		case captured && field == "type":
			valueString, valueCaptured, err := s.readResponsesInputOptionalString(value, depth)
			if err != nil {
				return shape, err
			}
			shape.formatType = ""
			if valueCaptured {
				shape.formatType = redactResponsesMetadataFormatType(strings.TrimSpace(valueString))
			}
		case captured && field == "syntax":
			valueString, valueCaptured, err := s.readResponsesInputOptionalString(value, depth)
			if err != nil {
				return shape, err
			}
			shape.grammarType = ""
			if valueCaptured {
				shape.grammarType = redactResponsesMetadataGrammarSyntax(strings.TrimSpace(valueString))
			}
		default:
			if err := s.skipValue(value, depth); err != nil {
				return shape, err
			}
		}

		next, err = s.nextNonWhitespace()
		if err != nil {
			return shape, err
		}
		switch next {
		case ',':
			next, err = s.nextNonWhitespace()
			if err != nil {
				return shape, err
			}
		case '}':
			return shape, nil
		default:
			return shape, errors.New("JSON object field is missing comma")
		}
	}
}

func (s *jsonTopLevelScanner) scanResponsesReasoningValue(first byte, depth int) (responsesReasoningShape, error) {
	if depth > 1000 {
		return responsesReasoningShape{}, errors.New("JSON nesting depth exceeds 1000")
	}
	if first == 'n' {
		if err := s.expectLiteral("ull"); err != nil {
			return responsesReasoningShape{}, err
		}
		return responsesReasoningShape{}, nil
	}
	shape := responsesReasoningShape{present: true}
	if first != '{' {
		if err := s.skipValue(first, depth); err != nil {
			return responsesReasoningShape{}, err
		}
		return shape, nil
	}
	shape.detailsKnown = true
	next, err := s.nextNonWhitespace()
	if err != nil {
		return responsesReasoningShape{}, err
	}
	if next == '}' {
		return shape, nil
	}
	for {
		if next != '"' {
			return responsesReasoningShape{}, errors.New("JSON object field name must be a string")
		}
		field, captured, err := s.readJSONString(maxTopLevelJSONStringCaptureBytes)
		if err != nil {
			return responsesReasoningShape{}, err
		}
		next, err = s.nextNonWhitespace()
		if err != nil {
			return responsesReasoningShape{}, err
		}
		if next != ':' {
			return responsesReasoningShape{}, errors.New("JSON object field is missing colon")
		}
		value, err := s.nextNonWhitespace()
		if err != nil {
			return responsesReasoningShape{}, err
		}

		switch {
		case captured && field == "context":
			present, err := s.scanResponsesNonNullValue(value, depth)
			if err != nil {
				return responsesReasoningShape{}, err
			}
			shape.hasContext = shape.hasContext || present
		case captured && field == "mode":
			present, err := s.scanResponsesNonNullValue(value, depth)
			if err != nil {
				return responsesReasoningShape{}, err
			}
			shape.hasMode = shape.hasMode || present
		default:
			if err := s.skipValue(value, depth); err != nil {
				return responsesReasoningShape{}, err
			}
		}

		next, err = s.nextNonWhitespace()
		if err != nil {
			return responsesReasoningShape{}, err
		}
		switch next {
		case ',':
			next, err = s.nextNonWhitespace()
			if err != nil {
				return responsesReasoningShape{}, err
			}
		case '}':
			return shape, nil
		default:
			return responsesReasoningShape{}, errors.New("JSON object field is missing comma")
		}
	}
}

func (s *jsonTopLevelScanner) scanResponsesNonNullValue(first byte, depth int) (bool, error) {
	if first == 'n' {
		if err := s.expectLiteral("ull"); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := s.skipValue(first, depth); err != nil {
		return false, err
	}
	return true, nil
}

func redactResponsesMetadataShapeType(value string, allowed map[string]struct{}) string {
	if _, ok := allowed[value]; ok {
		return value
	}
	return "other"
}

func redactResponsesMetadataFormatType(value string) string {
	switch value {
	case "grammar", "text":
		return value
	default:
		return "other"
	}
}

func redactResponsesMetadataGrammarSyntax(value string) string {
	switch value {
	case "lark", "regex":
		return value
	default:
		return "other"
	}
}

func isResponsesMetadataCompactionItemType(itemType string) bool {
	switch itemType {
	case "compaction", "context_compaction", "compaction_summary":
		return true
	default:
		return false
	}
}

func (s *jsonTopLevelScanner) readResponsesInputOptionalString(first byte, depth int) (string, bool, error) {
	if first == '"' {
		value, captured, err := s.readJSONString(maxTopLevelJSONStringCaptureBytes)
		if err != nil {
			return "", false, err
		}
		if !captured {
			return "", false, nil
		}
		return value, true, nil
	}
	if err := s.skipValue(first, depth); err != nil {
		return "", false, err
	}
	return "", false, nil
}
