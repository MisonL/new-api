package common

import (
	"errors"
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

const maxResponsesMetadataTrailingWhitespaceBytes = 64 * 1024
const maxResponsesMetadataEncryptedContentFingerprints = 32

var responsesMetadataInputShapeTypes = map[string]struct{}{
	"additional_tools":        {},
	"agent_message":           {},
	"compaction":              {},
	"compaction_trigger":      {},
	"computer_call":           {},
	"computer_call_output":    {},
	"custom_tool_call":        {},
	"custom_tool_call_output": {},
	"file_search_call":        {},
	"function_call":           {},
	"function_call_output":    {},
	"image_generation_call":   {},
	"item_reference":          {},
	"local_shell_call":        {},
	"local_shell_call_output": {},
	"mcp_approval_request":    {},
	"mcp_approval_response":   {},
	"mcp_call":                {},
	"mcp_list_tools":          {},
	"message":                 {},
	"reasoning":               {},
	"shell_call":              {},
	"shell_call_output":       {},
	"tool_search_call":        {},
	"tool_search_output":      {},
	"web_search_call":         {},
}

var responsesMetadataToolShapeTypes = map[string]struct{}{
	"computer_use_preview": {},
	"custom":               {},
	"file_search":          {},
	"function":             {},
	"image_generation":     {},
	"local_shell":          {},
	"mcp":                  {},
	"namespace":            {},
	"shell":                {},
	"tool_search":          {},
	"web_search":           {},
	"web_search_preview":   {},
}

// ResponsesJSONValueKind records the JSON shape of an inspected top-level field.
type ResponsesJSONValueKind string

const (
	ResponsesJSONValueKindMissing ResponsesJSONValueKind = ""
	ResponsesJSONValueKindNull    ResponsesJSONValueKind = "null"
	ResponsesJSONValueKindString  ResponsesJSONValueKind = "string"
	ResponsesJSONValueKindObject  ResponsesJSONValueKind = "object"
	ResponsesJSONValueKindArray   ResponsesJSONValueKind = "array"
	ResponsesJSONValueKindBoolean ResponsesJSONValueKind = "boolean"
	ResponsesJSONValueKindNumber  ResponsesJSONValueKind = "number"
)

// ResponsesRequestMetadata contains only the scalar routing and replay facts
// required before an opaque Responses body is forwarded. It never retains
// input, tools, or encrypted content.
type ResponsesRequestMetadata struct {
	Model              string
	ModelPresent       bool
	ModelLocation      TopLevelJSONStringLocation
	Group              string
	Stream             *bool
	MaxOutputTokens    *uint
	PreviousResponseID string

	InputPresent bool
	InputKind    ResponsesJSONValueKind
	ToolsPresent bool
	ToolsKind    ResponsesJSONValueKind

	HasOpaqueEncryptedContent             bool
	HasCompactionItem                     bool
	EncryptedContentFingerprints          []string
	EncryptedContentFingerprintsTruncated bool
	BodySize                              int64
	Shape                                 ResponsesRequestShapeMetadata
}

// ResponsesRequestShapeMetadata contains only redacted structural facts used
// for opaque Responses replay diagnostics. It never retains request text,
// tool names, tool schemas, encrypted content, or other arbitrary strings.
type ResponsesRequestShapeMetadata struct {
	InputItemCount      int
	InputItemCountKnown bool
	InputItemTypes      map[string]int

	HasAdditionalTools              bool
	AdditionalToolCount             int
	AdditionalToolCountKnown        bool
	AdditionalToolTypes             map[string]int
	AdditionalCustomFormatTypes     map[string]int
	AdditionalCustomGrammarSyntaxes map[string]int

	TopLevelToolCount             int
	TopLevelToolCountKnown        bool
	TopLevelToolTypes             map[string]int
	TopLevelCustomFormatTypes     map[string]int
	TopLevelCustomGrammarSyntaxes map[string]int

	HasCompactionTrigger  bool
	HasReasoning          bool
	ReasoningDetailsKnown bool
	HasReasoningContext   bool
	HasReasoningMode      bool
	HasInclude            bool
	HasToolChoice         bool
	HasParallelToolCalls  bool
	HasStore              bool
}

// Clone returns an independent copy of the bounded metadata. In particular,
// callers cannot mutate the redacted shape maps retained by an opaque request.
func (metadata ResponsesRequestMetadata) Clone() ResponsesRequestMetadata {
	cloned := metadata
	if metadata.Stream != nil {
		value := *metadata.Stream
		cloned.Stream = &value
	}
	if metadata.MaxOutputTokens != nil {
		value := *metadata.MaxOutputTokens
		cloned.MaxOutputTokens = &value
	}
	cloned.EncryptedContentFingerprints = append([]string(nil), metadata.EncryptedContentFingerprints...)
	cloned.Shape = metadata.Shape.clone()
	return cloned
}

func (shape ResponsesRequestShapeMetadata) clone() ResponsesRequestShapeMetadata {
	cloned := shape
	cloned.InputItemTypes = cloneResponsesShapeCountMap(shape.InputItemTypes)
	cloned.AdditionalToolTypes = cloneResponsesShapeCountMap(shape.AdditionalToolTypes)
	cloned.AdditionalCustomFormatTypes = cloneResponsesShapeCountMap(shape.AdditionalCustomFormatTypes)
	cloned.AdditionalCustomGrammarSyntaxes = cloneResponsesShapeCountMap(shape.AdditionalCustomGrammarSyntaxes)
	cloned.TopLevelToolTypes = cloneResponsesShapeCountMap(shape.TopLevelToolTypes)
	cloned.TopLevelCustomFormatTypes = cloneResponsesShapeCountMap(shape.TopLevelCustomFormatTypes)
	cloned.TopLevelCustomGrammarSyntaxes = cloneResponsesShapeCountMap(shape.TopLevelCustomGrammarSyntaxes)
	return cloned
}

func cloneResponsesShapeCountMap(source map[string]int) map[string]int {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]int, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

// SetResponsesRequestMetadata stores the bounded metadata collected for a
// disk-backed Responses request. The metadata never includes request content.
func SetResponsesRequestMetadata(c *gin.Context, metadata ResponsesRequestMetadata) {
	if c == nil {
		return
	}
	SetContextKey(c, constant.ContextKeyResponsesRequestMetadata, metadata)
}

// GetResponsesRequestMetadata returns the bounded metadata produced by the
// local Codex ingress, if that ingress handled the request.
func GetResponsesRequestMetadata(c *gin.Context) (ResponsesRequestMetadata, bool) {
	if c == nil {
		return ResponsesRequestMetadata{}, false
	}
	return GetContextKeyType[ResponsesRequestMetadata](c, constant.ContextKeyResponsesRequestMetadata)
}

// ScanResponsesRequestMetadata validates a Responses JSON object while
// retaining only metadata required by routing and opaque replay. It resets the
// source to its beginning before returning.
func ScanResponsesRequestMetadata(source io.ReadSeeker, bodySize int64) (ResponsesRequestMetadata, error) {
	if source == nil {
		return ResponsesRequestMetadata{}, errors.New("Responses request source is required")
	}
	if bodySize < 0 {
		return ResponsesRequestMetadata{}, errors.New("Responses request body size must not be negative")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return ResponsesRequestMetadata{}, fmt.Errorf("reset Responses request source: %w", err)
	}
	defer func() {
		_, _ = source.Seek(0, io.SeekStart)
	}()

	metadata := ResponsesRequestMetadata{BodySize: bodySize}
	scanner := newJSONTopLevelScanner(source)
	first, err := scanner.nextNonWhitespace()
	if err != nil {
		return ResponsesRequestMetadata{}, fmt.Errorf("read Responses JSON root: %w", err)
	}
	if first != '{' {
		return ResponsesRequestMetadata{}, errors.New("Responses request root must be an object")
	}

	next, err := scanner.nextNonWhitespace()
	if err != nil {
		return ResponsesRequestMetadata{}, fmt.Errorf("read Responses request field: %w", err)
	}
	if next != '}' {
		for {
			if next != '"' {
				return ResponsesRequestMetadata{}, errors.New("Responses request field name must be a string")
			}
			field, captured, err := scanner.readJSONString(maxTopLevelJSONStringCaptureBytes)
			if err != nil {
				return ResponsesRequestMetadata{}, fmt.Errorf("read Responses request field name: %w", err)
			}
			next, err = scanner.nextNonWhitespace()
			if err != nil {
				return ResponsesRequestMetadata{}, fmt.Errorf("read Responses request field separator: %w", err)
			}
			if next != ':' {
				return ResponsesRequestMetadata{}, errors.New("Responses request field is missing colon")
			}
			value, err := scanner.nextNonWhitespace()
			if err != nil {
				return ResponsesRequestMetadata{}, fmt.Errorf("read Responses request field value: %w", err)
			}

			if captured {
				if err := scanResponsesTopLevelField(scanner, field, value, &metadata); err != nil {
					return ResponsesRequestMetadata{}, err
				}
			} else if err := scanner.skipValue(value, 1); err != nil {
				return ResponsesRequestMetadata{}, fmt.Errorf("skip oversized Responses request field: %w", err)
			}

			next, err = scanner.nextNonWhitespace()
			if err != nil {
				return ResponsesRequestMetadata{}, fmt.Errorf("read Responses request field delimiter: %w", err)
			}
			switch next {
			case ',':
				next, err = scanner.nextNonWhitespace()
				if err != nil {
					return ResponsesRequestMetadata{}, fmt.Errorf("read next Responses request field: %w", err)
				}
			case '}':
				goto complete
			default:
				return ResponsesRequestMetadata{}, errors.New("Responses request field is missing comma")
			}
		}
	}

complete:
	if err := scanner.expectEOF(); err != nil {
		return ResponsesRequestMetadata{}, err
	}
	if scanner.offset != bodySize {
		return ResponsesRequestMetadata{}, fmt.Errorf("Responses request body size mismatch: scanned=%d expected=%d", scanner.offset, bodySize)
	}
	return metadata, nil
}

func scanResponsesTopLevelField(scanner *jsonTopLevelScanner, field string, first byte, metadata *ResponsesRequestMetadata) error {
	if scanner == nil || metadata == nil {
		return errors.New("Responses metadata scanner is required")
	}
	switch field {
	case "model":
		metadata.ModelPresent = true
		value, location, err := scanner.readResponsesNullableString(first, field)
		if err != nil {
			return err
		}
		metadata.Model = value
		metadata.ModelLocation = location
	case "group":
		value, _, err := scanner.readResponsesNullableString(first, field)
		if err != nil {
			return err
		}
		metadata.Group = value
	case "previous_response_id":
		value, _, err := scanner.readResponsesNullableString(first, field)
		if err != nil {
			return err
		}
		metadata.PreviousResponseID = value
	case "stream":
		value, err := scanner.readResponsesNullableBool(first, field)
		if err != nil {
			return err
		}
		metadata.Stream = value
	case "max_output_tokens":
		value, err := scanner.readResponsesNullableUint(first, field)
		if err != nil {
			return err
		}
		metadata.MaxOutputTokens = value
	case "input":
		result, err := scanner.scanResponsesInputValue(first, 1, true, true, first == '[')
		if err != nil {
			return fmt.Errorf("scan Responses input: %w", err)
		}
		metadata.InputPresent = true
		metadata.InputKind = jsonValueKind(first)
		metadata.mergeInputScanResult(result)
		metadata.Shape.setInputShape(result.shape)
	case "tools":
		metadata.ToolsPresent = true
		metadata.ToolsKind = jsonValueKind(first)
		toolShape, err := scanner.scanResponsesToolsValue(first, 1)
		if err != nil {
			return fmt.Errorf("scan Responses tools: %w", err)
		}
		metadata.Shape.setTopLevelToolShape(toolShape)
	case "reasoning":
		reasoningShape, err := scanner.scanResponsesReasoningValue(first, 1)
		if err != nil {
			return fmt.Errorf("scan Responses reasoning: %w", err)
		}
		metadata.Shape.setReasoningShape(reasoningShape)
	case "include":
		present, err := scanner.scanResponsesNonNullValue(first, 1)
		if err != nil {
			return fmt.Errorf("scan Responses include: %w", err)
		}
		metadata.Shape.HasInclude = present
	case "tool_choice":
		present, err := scanner.scanResponsesNonNullValue(first, 1)
		if err != nil {
			return fmt.Errorf("scan Responses tool_choice: %w", err)
		}
		metadata.Shape.HasToolChoice = present
	case "parallel_tool_calls":
		present, err := scanner.scanResponsesNonNullValue(first, 1)
		if err != nil {
			return fmt.Errorf("scan Responses parallel_tool_calls: %w", err)
		}
		metadata.Shape.HasParallelToolCalls = present
	case "store":
		present, err := scanner.scanResponsesNonNullValue(first, 1)
		if err != nil {
			return fmt.Errorf("scan Responses store: %w", err)
		}
		metadata.Shape.HasStore = present
	default:
		if err := scanner.skipValue(first, 1); err != nil {
			return fmt.Errorf("skip Responses request field %q: %w", field, err)
		}
	}
	return nil
}

func (metadata *ResponsesRequestMetadata) mergeInputScanResult(result responsesInputScanResult) {
	if metadata == nil {
		return
	}
	merged := responsesInputScanResult{
		hasOpaqueEncryptedContent:             metadata.HasOpaqueEncryptedContent,
		hasCompactionItem:                     metadata.HasCompactionItem,
		encryptedContentFingerprints:          metadata.EncryptedContentFingerprints,
		encryptedContentFingerprintsTruncated: metadata.EncryptedContentFingerprintsTruncated,
	}
	merged.merge(result)
	metadata.HasOpaqueEncryptedContent = merged.hasOpaqueEncryptedContent
	metadata.HasCompactionItem = merged.hasCompactionItem
	metadata.EncryptedContentFingerprints = merged.encryptedContentFingerprints
	metadata.EncryptedContentFingerprintsTruncated = merged.encryptedContentFingerprintsTruncated
}

func (s *jsonTopLevelScanner) readResponsesNullableString(first byte, field string) (string, TopLevelJSONStringLocation, error) {
	if first == 'n' {
		if err := s.expectLiteral("ull"); err != nil {
			return "", TopLevelJSONStringLocation{}, fmt.Errorf("read Responses field %q: %w", field, err)
		}
		return "", TopLevelJSONStringLocation{}, nil
	}
	if first != '"' {
		return "", TopLevelJSONStringLocation{}, fmt.Errorf("Responses field %q must be a string", field)
	}
	start := s.offset - 1
	value, captured, err := s.readJSONString(maxTopLevelJSONStringCaptureBytes)
	if err != nil {
		return "", TopLevelJSONStringLocation{}, fmt.Errorf("read Responses field %q: %w", field, err)
	}
	if !captured {
		return "", TopLevelJSONStringLocation{}, fmt.Errorf("Responses field %q exceeds %d bytes", field, maxTopLevelJSONStringCaptureBytes)
	}
	return value, TopLevelJSONStringLocation{Start: start, End: s.offset, Value: value}, nil
}

func (s *jsonTopLevelScanner) readResponsesNullableBool(first byte, field string) (*bool, error) {
	switch first {
	case 'n':
		if err := s.expectLiteral("ull"); err != nil {
			return nil, fmt.Errorf("read Responses field %q: %w", field, err)
		}
		return nil, nil
	case 't':
		if err := s.expectLiteral("rue"); err != nil {
			return nil, fmt.Errorf("read Responses field %q: %w", field, err)
		}
		value := true
		return &value, nil
	case 'f':
		if err := s.expectLiteral("alse"); err != nil {
			return nil, fmt.Errorf("read Responses field %q: %w", field, err)
		}
		value := false
		return &value, nil
	default:
		return nil, fmt.Errorf("Responses field %q must be a boolean", field)
	}
}

func (s *jsonTopLevelScanner) readResponsesNullableUint(first byte, field string) (*uint, error) {
	if first == 'n' {
		if err := s.expectLiteral("ull"); err != nil {
			return nil, fmt.Errorf("read Responses field %q: %w", field, err)
		}
		return nil, nil
	}
	if first < '0' || first > '9' {
		return nil, fmt.Errorf("Responses field %q must be an unsigned integer", field)
	}
	number, err := s.readJSONNumberToken(first)
	if err != nil {
		return nil, fmt.Errorf("read Responses field %q: %w", field, err)
	}
	var value uint
	if err := Unmarshal([]byte(number), &value); err != nil {
		return nil, fmt.Errorf("Responses field %q must be an unsigned integer: %w", field, err)
	}
	return &value, nil
}

func (s *jsonTopLevelScanner) readJSONNumberToken(first byte) (string, error) {
	var raw []byte
	raw = append(raw, first)
	for {
		next, err := s.peekByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if isJSONValueDelimiter(next) {
			break
		}
		_, err = s.readByte()
		if err != nil {
			return "", err
		}
		raw = append(raw, next)
	}
	return string(raw), nil
}

func jsonValueKind(first byte) ResponsesJSONValueKind {
	switch first {
	case 'n':
		return ResponsesJSONValueKindNull
	case '"':
		return ResponsesJSONValueKindString
	case '{':
		return ResponsesJSONValueKindObject
	case '[':
		return ResponsesJSONValueKindArray
	case 't', 'f':
		return ResponsesJSONValueKindBoolean
	default:
		return ResponsesJSONValueKindNumber
	}
}
