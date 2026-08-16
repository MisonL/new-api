package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	CodexLocalCompactionIngressReductionThresholdBytes = CodexLocalCompaction413RecoverySafeBodyMaxBytes
	codexLocalCompactionIngressTokenMaxBytes           = 64 * 1024
	codexLocalCompactionIngressInputItemMaxBytes       = 32 * 1024 * 1024
)

var ErrCodexLocalCompactionIngressValueTooLarge = errors.New("codex local compaction ingress value exceeds limit")
var ErrCodexLocalCompactionOpaqueEncryptedReplay = errors.New("codex local compaction request with opaque encrypted_content cannot be safely reduced")

type CodexLocalCompactionIngressReductionInfo struct {
	CodexLocalCompaction413RecoveryInfo
	InputItemCount int
}

type codexLocalCompactionIngressCountingReader struct {
	reader io.Reader
	bytes  int64
}

func (reader *codexLocalCompactionIngressCountingReader) Read(data []byte) (int, error) {
	n, err := reader.reader.Read(data)
	reader.bytes += int64(n)
	return n, err
}

type codexLocalCompactionIngressCapture struct {
	request        dto.OpenAIResponsesRequest
	inputSeen      bool
	inputBytes     int
	inputItemCount int
	visibleParts   int
	visibleTail    *visibleResponsesInputTail
	toolsSeen      bool
	toolsJSONType  string
}

type codexLocalCompactionIngressInputToolState struct {
	itemType      string
	toolsSeen     bool
	toolsJSONType string
}

// PrepareCodexLocalCompactionIngressRequest preserves a normal-sized local
// compaction request byte-for-byte. Once the request crosses the safe upstream
// body limit, it replays the bounded prefix into the streaming reducer so the
// full history never reaches regular body storage or DTO decoding.
func PrepareCodexLocalCompactionIngressRequest(reader io.Reader) ([]byte, bool, CodexLocalCompactionIngressReductionInfo, error) {
	if reader == nil {
		return nil, false, CodexLocalCompactionIngressReductionInfo{}, errors.New("codex local compaction ingress request body is required")
	}

	probe, err := io.ReadAll(io.LimitReader(reader, int64(CodexLocalCompactionIngressReductionThresholdBytes)+1))
	if err != nil {
		return nil, false, CodexLocalCompactionIngressReductionInfo{}, fmt.Errorf("read codex local compaction ingress body: %w", err)
	}
	if len(probe) <= CodexLocalCompactionIngressReductionThresholdBytes {
		return probe, false, CodexLocalCompactionIngressReductionInfo{
			CodexLocalCompaction413RecoveryInfo: CodexLocalCompaction413RecoveryInfo{
				OriginalRequestBytes:  int64(len(probe)),
				RecoveredRequestBytes: len(probe),
			},
		}, nil
	}

	body, info, err := reduceCodexLocalCompactionIngressRequest(io.MultiReader(bytes.NewReader(probe), reader))
	if err != nil {
		return nil, false, info, err
	}
	return body, true, info, nil
}

func reduceCodexLocalCompactionIngressRequest(reader io.Reader) ([]byte, CodexLocalCompactionIngressReductionInfo, error) {
	countingReader := &codexLocalCompactionIngressCountingReader{reader: reader}
	jsonReader := newCodexLocalCompactionIngressJSONReader(countingReader)
	capture := codexLocalCompactionIngressCapture{
		visibleTail: newVisibleResponsesInputTail(syntheticCompactVisibleTextMax),
	}

	if err := jsonReader.skipSpace(); err != nil {
		return nil, CodexLocalCompactionIngressReductionInfo{}, fmt.Errorf("read codex local compaction ingress request: %w", err)
	}
	token, err := jsonReader.readByte(nil)
	if err != nil {
		return nil, CodexLocalCompactionIngressReductionInfo{}, fmt.Errorf("read codex local compaction ingress request: %w", err)
	}
	if token != '{' {
		return nil, CodexLocalCompactionIngressReductionInfo{}, errors.New("codex local compaction ingress request must be a JSON object")
	}

	if err := capture.readObject(jsonReader); err != nil {
		return nil, CodexLocalCompactionIngressReductionInfo{}, err
	}
	if err := capture.validateDiscardedTools(); err != nil {
		return nil, CodexLocalCompactionIngressReductionInfo{}, err
	}
	if err := jsonReader.expectEnd(); err != nil {
		return nil, CodexLocalCompactionIngressReductionInfo{}, err
	}
	if jsonReader.hasOpaqueEncryptedContent {
		return nil, CodexLocalCompactionIngressReductionInfo{
			CodexLocalCompaction413RecoveryInfo: CodexLocalCompaction413RecoveryInfo{
				OriginalRequestBytes: countingReader.bytes,
				OriginalInputBytes:   capture.inputBytes,
			},
			InputItemCount: capture.inputItemCount,
		}, ErrCodexLocalCompactionOpaqueEncryptedReplay
	}

	if capture.request.Model == "" {
		return nil, CodexLocalCompactionIngressReductionInfo{}, errors.New("codex local compaction ingress request model is required")
	}
	if !capture.inputSeen {
		return nil, CodexLocalCompactionIngressReductionInfo{}, errors.New("codex local compaction ingress request input is required")
	}

	visibleParts := capture.visibleTail.parts()
	reduced, recoveryInfo, err := buildCodexLocalCompaction413RecoveryRequestFromVisibleParts(
		capture.request,
		visibleParts,
		capture.inputBytes,
		CodexLocalCompaction413RecoveryStageBoundedVisible,
	)
	if err != nil {
		return nil, CodexLocalCompactionIngressReductionInfo{}, fmt.Errorf("%w: build codex local compaction ingress reduction: %v", ErrCodexLocalCompactionIngressValueTooLarge, err)
	}
	body, err := common.Marshal(reduced)
	if err != nil {
		return nil, CodexLocalCompactionIngressReductionInfo{}, fmt.Errorf("marshal codex local compaction ingress reduction: %w", err)
	}
	if len(body) > CodexLocalCompaction413RecoverySafeBodyMaxBytes {
		return nil, CodexLocalCompactionIngressReductionInfo{}, fmt.Errorf(
			"%w: reduced request exceeds %d bytes",
			ErrCodexLocalCompactionIngressValueTooLarge,
			CodexLocalCompaction413RecoverySafeBodyMaxBytes,
		)
	}

	recoveryInfo.OriginalRequestBytes = countingReader.bytes
	recoveryInfo.RecoveredRequestBytes = len(body)
	return body, CodexLocalCompactionIngressReductionInfo{
		CodexLocalCompaction413RecoveryInfo: recoveryInfo,
		InputItemCount:                      capture.inputItemCount,
	}, nil
}

func (capture *codexLocalCompactionIngressCapture) readObject(reader *codexLocalCompactionIngressJSONReader) error {
	if err := reader.skipSpace(); err != nil {
		return err
	}
	if closed, err := reader.consumeByte(nil, '}'); err != nil {
		return err
	} else if closed {
		return nil
	}
	for {
		field, err := reader.readString(codexLocalCompactionIngressTokenMaxBytes, "request field")
		if err != nil {
			return err
		}
		if field == "" {
			return errors.New("codex local compaction ingress request contains an invalid field name")
		}
		if err := reader.skipSpace(); err != nil {
			return err
		}
		if err := reader.expectByte(nil, ':'); err != nil {
			return err
		}
		if err := capture.readField(reader, field); err != nil {
			return err
		}
		if err := reader.skipSpace(); err != nil {
			return err
		}
		delimiter, err := reader.readByte(nil)
		if err != nil {
			return err
		}
		switch delimiter {
		case '}':
			return nil
		case ',':
			if err := reader.skipSpace(); err != nil {
				return err
			}
		default:
			return errors.New("codex local compaction ingress request has an invalid JSON object terminator")
		}
	}
}

func (capture *codexLocalCompactionIngressCapture) readField(reader *codexLocalCompactionIngressJSONReader, field string) error {
	switch field {
	case "input":
		return capture.readInput(reader)
	case "model":
		return decodeCodexLocalCompactionIngressValue(reader, field, codexLocalCompactionIngressTokenMaxBytes, &capture.request.Model)
	case "include":
		return decodeCodexLocalCompactionIngressRawValue(reader, field, codexLocalCompaction413RecoveryRawFieldMaxBytes, &capture.request.Include)
	case "instructions":
		return decodeCodexLocalCompactionIngressRawValue(reader, field, codexLocalCompaction413RecoveryRawFieldMaxBytes, &capture.request.Instructions)
	case "max_output_tokens":
		return decodeCodexLocalCompactionIngressValue(reader, field, codexLocalCompactionIngressTokenMaxBytes, &capture.request.MaxOutputTokens)
	case "top_logprobs":
		return decodeCodexLocalCompactionIngressValue(reader, field, codexLocalCompactionIngressTokenMaxBytes, &capture.request.TopLogProbs)
	case "metadata":
		return decodeCodexLocalCompactionIngressRawValue(reader, field, codexLocalCompaction413RecoveryRawFieldMaxBytes, &capture.request.Metadata)
	case "reasoning":
		return decodeCodexLocalCompactionIngressValue(reader, field, codexLocalCompaction413RecoveryRawFieldMaxBytes, &capture.request.Reasoning)
	case "service_tier":
		return decodeCodexLocalCompactionIngressValue(reader, field, codexLocalCompactionIngressTokenMaxBytes, &capture.request.ServiceTier)
	case "store":
		return decodeCodexLocalCompactionIngressRawValue(reader, field, codexLocalCompaction413RecoveryRawFieldMaxBytes, &capture.request.Store)
	case "stream":
		return decodeCodexLocalCompactionIngressValue(reader, field, codexLocalCompactionIngressTokenMaxBytes, &capture.request.Stream)
	case "stream_options":
		return decodeCodexLocalCompactionIngressValue(reader, field, codexLocalCompaction413RecoveryRawFieldMaxBytes, &capture.request.StreamOptions)
	case "temperature":
		return decodeCodexLocalCompactionIngressValue(reader, field, codexLocalCompactionIngressTokenMaxBytes, &capture.request.Temperature)
	case "text":
		return decodeCodexLocalCompactionIngressRawValue(reader, field, codexLocalCompaction413RecoveryRawFieldMaxBytes, &capture.request.Text)
	case "top_p":
		return decodeCodexLocalCompactionIngressValue(reader, field, codexLocalCompactionIngressTokenMaxBytes, &capture.request.TopP)
	case "truncation":
		return decodeCodexLocalCompactionIngressRawValue(reader, field, codexLocalCompaction413RecoveryRawFieldMaxBytes, &capture.request.Truncation)
	case "safety_identifier":
		return decodeCodexLocalCompactionIngressRawValue(reader, field, codexLocalCompaction413RecoveryRawFieldMaxBytes, &capture.request.SafetyIdentifier)
	case "tools":
		return capture.readTopLevelTools(reader)
	case "conversation", "context_management", "parallel_tool_calls", "previous_response_id", "prompt_cache_key", "prompt_cache_retention", "tool_choice", "additional_tools", "user", "max_tool_calls", "prompt", "enable_thinking", "preset":
		return reader.discardValue()
	default:
		return capture.readExtraField(reader, field)
	}
}

func (capture *codexLocalCompactionIngressCapture) readTopLevelTools(reader *codexLocalCompactionIngressJSONReader) error {
	jsonType, err := reader.discardValueJSONType()
	if err != nil {
		return err
	}
	capture.toolsSeen = true
	capture.toolsJSONType = jsonType
	return nil
}

func (capture *codexLocalCompactionIngressCapture) validateDiscardedTools() error {
	if capture.toolsSeen && capture.toolsJSONType != "array" && capture.toolsJSONType != "null" {
		return errors.New("tools must be an array")
	}
	return nil
}

func (capture *codexLocalCompactionIngressCapture) readInput(reader *codexLocalCompactionIngressJSONReader) error {
	if capture.inputSeen {
		return errors.New("codex local compaction ingress request contains duplicate input fields")
	}
	capture.inputSeen = true
	if err := reader.skipSpace(); err != nil {
		return err
	}
	token, err := reader.readByte(nil)
	if err != nil {
		return fmt.Errorf("read codex local compaction ingress input: %w", err)
	}
	if token != '[' {
		return errors.New("codex local compaction ingress request input must be an array")
	}
	if err := reader.skipSpace(); err != nil {
		return err
	}
	if closed, err := reader.consumeByte(nil, ']'); err != nil {
		return err
	} else if closed {
		return nil
	}
	for {
		rawItem, inputItemBytes, err := reader.readInputItem()
		if err != nil {
			return err
		}
		capture.inputBytes += inputItemBytes
		capture.inputItemCount++
		visiblePart := rawVisibleResponsesInputItem(rawItem)
		if strings.TrimSpace(visiblePart) != "" {
			capture.visibleTail.append(visiblePart)
			capture.visibleParts++
		}
		if err := reader.skipSpace(); err != nil {
			return err
		}
		delimiter, err := reader.readByte(nil)
		if err != nil {
			return err
		}
		switch delimiter {
		case ']':
			return nil
		case ',':
			if err := reader.skipSpace(); err != nil {
				return err
			}
		default:
			return errors.New("codex local compaction ingress request input has an invalid JSON array terminator")
		}
	}
}

func (reader *codexLocalCompactionIngressJSONReader) readInputItem() (common.RawMessage, int, error) {
	start := reader.bytesRead
	first, err := reader.readByte(nil)
	if err != nil {
		return nil, 0, err
	}

	var rawItem common.RawMessage
	if first == '{' {
		rawItem, err = reader.readInputObjectWithoutTools()
	} else {
		rawItem, err = reader.copyValueFromFirstIntoRaw(
			first,
			codexLocalCompactionIngressInputItemMaxBytes,
			"input item",
		)
	}
	if err != nil {
		return nil, 0, err
	}
	return rawItem, int(reader.bytesRead - start), nil
}

func (reader *codexLocalCompactionIngressJSONReader) readInputObjectWithoutTools() (common.RawMessage, error) {
	capture := &codexLocalCompactionIngressJSONCapture{
		max:     codexLocalCompactionIngressInputItemMaxBytes,
		subject: "input item",
	}
	toolState := codexLocalCompactionIngressInputToolState{}
	if err := capture.writeByte('{'); err != nil {
		return nil, err
	}
	if err := reader.skipSpace(); err != nil {
		return nil, err
	}
	if closed, err := reader.consumeByte(nil, '}'); err != nil {
		return nil, err
	} else if closed {
		if err := capture.writeByte('}'); err != nil {
			return nil, err
		}
		return append(common.RawMessage(nil), capture.data.Bytes()...), nil
	}

	for {
		field, err := reader.readString(codexLocalCompactionIngressTokenMaxBytes, "input item field")
		if err != nil {
			return nil, err
		}
		if err := reader.skipSpace(); err != nil {
			return nil, err
		}
		if err := reader.expectByte(nil, ':'); err != nil {
			return nil, err
		}
		if err := reader.readInputObjectField(capture, &toolState, field); err != nil {
			return nil, err
		}
		if err := reader.skipSpace(); err != nil {
			return nil, err
		}
		delimiter, err := reader.readByte(nil)
		if err != nil {
			return nil, err
		}
		switch delimiter {
		case '}':
			if err := toolState.validate(); err != nil {
				return nil, err
			}
			if err := capture.writeByte('}'); err != nil {
				return nil, err
			}
			return append(common.RawMessage(nil), capture.data.Bytes()...), nil
		case ',':
			if err := reader.skipSpace(); err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("codex local compaction ingress input item has an invalid JSON object terminator")
		}
	}
}

func (reader *codexLocalCompactionIngressJSONReader) readInputObjectField(capture *codexLocalCompactionIngressJSONCapture, toolState *codexLocalCompactionIngressInputToolState, field string) error {
	switch field {
	case "tools":
		jsonType, err := reader.discardValueJSONType()
		if err != nil {
			return err
		}
		toolState.toolsSeen = true
		toolState.toolsJSONType = jsonType
		return nil
	case "additional_tools":
		return reader.discardValue()
	case "type":
		return reader.readInputObjectType(capture, toolState, field)
	default:
		return reader.readInputObjectRetainedField(capture, field)
	}
}

func (reader *codexLocalCompactionIngressJSONReader) readInputObjectType(capture *codexLocalCompactionIngressJSONCapture, toolState *codexLocalCompactionIngressInputToolState, field string) error {
	encodedField, err := common.Marshal(field)
	if err != nil {
		return fmt.Errorf("marshal codex local compaction ingress input item field: %w", err)
	}
	maxValueBytes, err := capture.objectFieldValueMaxBytes(encodedField)
	if err != nil {
		return err
	}
	rawValue, err := reader.readRawValue(maxValueBytes, "input item type")
	if err != nil {
		return err
	}
	switch common.GetJsonType(rawValue) {
	case "string":
		if err := common.Unmarshal(rawValue, &toolState.itemType); err != nil {
			return fmt.Errorf("decode codex local compaction ingress input item type: %w", err)
		}
		toolState.itemType = strings.TrimSpace(toolState.itemType)
	case "null":
		// encoding/json leaves a string field unchanged for a null value.
	default:
		return fmt.Errorf("decode codex local compaction ingress input item type: expected JSON string")
	}
	return capture.appendObjectField(encodedField, rawValue)
}

func (reader *codexLocalCompactionIngressJSONReader) readInputObjectRetainedField(capture *codexLocalCompactionIngressJSONCapture, field string) error {
	encodedField, err := common.Marshal(field)
	if err != nil {
		return fmt.Errorf("marshal codex local compaction ingress input item field: %w", err)
	}
	maxValueBytes, err := capture.objectFieldValueMaxBytes(encodedField)
	if err != nil {
		return err
	}
	rawValue, err := reader.readRawValueForField(maxValueBytes, "input item", field)
	if err != nil {
		return err
	}
	return capture.appendObjectField(encodedField, rawValue)
}

func (state codexLocalCompactionIngressInputToolState) validate() error {
	switch state.itemType {
	case "additional_tools", "tool_search_output":
		if !state.toolsSeen || state.toolsJSONType == "null" {
			return fmt.Errorf("%s.tools is required", state.itemType)
		}
		if state.toolsJSONType != "array" {
			return fmt.Errorf("%s.tools must be an array", state.itemType)
		}
	}
	return nil
}

func (capture *codexLocalCompactionIngressCapture) readExtraField(reader *codexLocalCompactionIngressJSONReader, field string) error {
	raw, err := reader.readRawValueForField(codexLocalCompaction413RecoveryExtraMaxBytes, field, field)
	if err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if capture.request.Extra == nil {
		capture.request.Extra = make(map[string]common.RawMessage)
	}
	used := 0
	for key, value := range capture.request.Extra {
		used += len(key) + len(value)
	}
	if used+len(field)+len(trimmed) > codexLocalCompaction413RecoveryExtraMaxBytes {
		return fmt.Errorf("%w: retained extra fields exceed %d bytes", ErrCodexLocalCompactionIngressValueTooLarge, codexLocalCompaction413RecoveryExtraMaxBytes)
	}
	capture.request.Extra[field] = append(common.RawMessage(nil), trimmed...)
	return nil
}

func decodeCodexLocalCompactionIngressRawValue(reader *codexLocalCompactionIngressJSONReader, field string, maxBytes int, destination *common.RawMessage) error {
	raw, err := reader.readRawValueForField(maxBytes, field, field)
	if err != nil {
		return err
	}
	*destination = append((*destination)[:0], raw...)
	return nil
}

func decodeCodexLocalCompactionIngressValue(reader *codexLocalCompactionIngressJSONReader, field string, maxBytes int, destination any) error {
	raw, err := reader.readRawValueForField(maxBytes, field, field)
	if err != nil {
		return err
	}
	if err := common.Unmarshal(raw, destination); err != nil {
		return fmt.Errorf("decode codex local compaction ingress %s: %w", field, err)
	}
	return nil
}
