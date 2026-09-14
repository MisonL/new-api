package service

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const codexLocalCompactionIngressJSONNestingDepthMax = syntheticCompactJSONNestingDepthMax

// codexLocalCompactionIngressJSONReader validates a single JSON document while
// allowing explicitly discarded fields to be consumed without buffering them.
// The ordinary encoding/json decoder must first buffer a whole value, which is
// unsuitable for a large tools schema that local compaction will not replay.
type codexLocalCompactionIngressJSONReader struct {
	reader                    *bufio.Reader
	bytesRead                 int64
	hasOpaqueEncryptedContent bool
}

type codexLocalCompactionIngressJSONCapture struct {
	data    bytes.Buffer
	max     int
	subject string
}

func newCodexLocalCompactionIngressJSONReader(reader io.Reader) *codexLocalCompactionIngressJSONReader {
	return &codexLocalCompactionIngressJSONReader{reader: bufio.NewReader(reader)}
}

func (reader *codexLocalCompactionIngressJSONReader) readString(maxBytes int, subject string) (string, error) {
	capture := &codexLocalCompactionIngressJSONCapture{max: maxBytes, subject: subject}
	if err := reader.skipSpace(); err != nil {
		return "", err
	}
	first, err := reader.readByte(capture)
	if err != nil {
		return "", err
	}
	if first != '"' {
		return "", fmt.Errorf("codex local compaction ingress %s must be a JSON string", subject)
	}
	if err := reader.copyStringTail(capture); err != nil {
		return "", err
	}
	var value string
	if err := common.Unmarshal(capture.data.Bytes(), &value); err != nil {
		return "", fmt.Errorf("decode codex local compaction ingress %s: %w", subject, err)
	}
	return value, nil
}

func (reader *codexLocalCompactionIngressJSONReader) readRawValue(maxBytes int, subject string) (common.RawMessage, error) {
	capture := &codexLocalCompactionIngressJSONCapture{max: maxBytes, subject: subject}
	if err := reader.copyValue(capture, 0); err != nil {
		return nil, err
	}
	return append(common.RawMessage(nil), capture.data.Bytes()...), nil
}

func (reader *codexLocalCompactionIngressJSONReader) readRawValueForField(maxBytes int, subject string, field string) (common.RawMessage, error) {
	if field != "encrypted_content" {
		return reader.readRawValue(maxBytes, subject)
	}
	if err := reader.skipSpace(); err != nil {
		return nil, err
	}
	first, err := reader.readByte(nil)
	if err != nil {
		return nil, err
	}
	return reader.copyEncryptedContentValueFromFirst(nil, first, 0, maxBytes, subject)
}

func (reader *codexLocalCompactionIngressJSONReader) discardValue() error {
	return reader.copyValue(nil, 0)
}

func (reader *codexLocalCompactionIngressJSONReader) discardValueJSONType() (string, error) {
	if err := reader.skipSpace(); err != nil {
		return "", err
	}
	first, err := reader.readByte(nil)
	if err != nil {
		return "", err
	}
	jsonType := codexLocalCompactionIngressJSONValueType(first)
	if err := reader.copyValueFromFirst(nil, first, 0); err != nil {
		return "", err
	}
	return jsonType, nil
}

func (reader *codexLocalCompactionIngressJSONReader) copyValue(capture *codexLocalCompactionIngressJSONCapture, depth int) error {
	if err := reader.skipSpace(); err != nil {
		return err
	}
	first, err := reader.readByte(capture)
	if err != nil {
		return err
	}
	return reader.copyValueFromFirst(capture, first, depth)
}

func (reader *codexLocalCompactionIngressJSONReader) copyValueFromFirstIntoRaw(first byte, maxBytes int, subject string) (common.RawMessage, error) {
	capture := &codexLocalCompactionIngressJSONCapture{max: maxBytes, subject: subject}
	if err := capture.writeByte(first); err != nil {
		return nil, err
	}
	if err := reader.copyValueFromFirst(capture, first, 0); err != nil {
		return nil, err
	}
	return append(common.RawMessage(nil), capture.data.Bytes()...), nil
}

func (reader *codexLocalCompactionIngressJSONReader) copyValueFromFirst(capture *codexLocalCompactionIngressJSONCapture, first byte, depth int) error {
	if depth > codexLocalCompactionIngressJSONNestingDepthMax {
		return fmt.Errorf("%w: JSON nesting exceeds %d", ErrCodexLocalCompactionIngressValueTooLarge, codexLocalCompactionIngressJSONNestingDepthMax)
	}
	switch first {
	case '{':
		return reader.copyObject(capture, depth+1)
	case '[':
		return reader.copyArray(capture, depth+1)
	case '"':
		return reader.copyStringTail(capture)
	case 't':
		return reader.copyLiteralTail(capture, "rue")
	case 'f':
		return reader.copyLiteralTail(capture, "alse")
	case 'n':
		return reader.copyLiteralTail(capture, "ull")
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return reader.copyNumberTail(capture, first)
	default:
		return fmt.Errorf("invalid JSON value in codex local compaction ingress request")
	}
}

func (reader *codexLocalCompactionIngressJSONReader) copyObject(capture *codexLocalCompactionIngressJSONCapture, depth int) error {
	if err := reader.copySpace(capture); err != nil {
		return err
	}
	if closed, err := reader.consumeByte(capture, '}'); err != nil {
		return err
	} else if closed {
		return nil
	}
	for {
		field, err := reader.readObjectField(capture)
		if err != nil {
			return err
		}
		if err := reader.copySpace(capture); err != nil {
			return err
		}
		if err := reader.expectByte(capture, ':'); err != nil {
			return err
		}
		if err := reader.copySpace(capture); err != nil {
			return err
		}
		first, err := reader.readByte(nil)
		if err != nil {
			return err
		}
		if field == "encrypted_content" {
			if _, err := reader.copyEncryptedContentValueFromFirst(
				capture,
				first,
				depth,
				codexLocalCompactionIngressInputItemMaxBytes,
				"encrypted_content",
			); err != nil {
				return err
			}
		} else {
			if err := capture.writeByte(first); err != nil {
				return err
			}
			if err := reader.copyValueFromFirst(capture, first, depth); err != nil {
				return err
			}
		}
		if err := reader.copySpace(capture); err != nil {
			return err
		}
		delimiter, err := reader.readByte(capture)
		if err != nil {
			return err
		}
		switch delimiter {
		case '}':
			return nil
		case ',':
			if err := reader.copySpace(capture); err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid JSON object delimiter in codex local compaction ingress request")
		}
	}
}

func (reader *codexLocalCompactionIngressJSONReader) readObjectField(capture *codexLocalCompactionIngressJSONCapture) (string, error) {
	first, err := reader.readByte(nil)
	if err != nil {
		return "", err
	}
	if first != '"' {
		return "", fmt.Errorf("invalid JSON object key in codex local compaction ingress request")
	}
	raw, err := reader.copyValueFromFirstIntoRaw(
		first,
		codexLocalCompactionIngressTokenMaxBytes,
		"JSON object key",
	)
	if err != nil {
		return "", err
	}
	if err := capture.writeBytes(raw); err != nil {
		return "", err
	}
	var field string
	if err := common.Unmarshal(raw, &field); err != nil {
		return "", fmt.Errorf("decode codex local compaction ingress JSON object key: %w", err)
	}
	return field, nil
}

func (reader *codexLocalCompactionIngressJSONReader) copyEncryptedContentValueFromFirst(capture *codexLocalCompactionIngressJSONCapture, first byte, depth int, maxBytes int, subject string) (common.RawMessage, error) {
	valueCapture := &codexLocalCompactionIngressJSONCapture{max: maxBytes, subject: subject}
	if err := valueCapture.writeByte(first); err != nil {
		return nil, err
	}
	if err := reader.copyValueFromFirst(valueCapture, first, depth); err != nil {
		return nil, err
	}
	raw := append(common.RawMessage(nil), valueCapture.data.Bytes()...)
	if err := capture.writeBytes(raw); err != nil {
		return nil, err
	}
	switch common.GetJsonType(raw) {
	case "null":
		return raw, nil
	case "string":
		var value string
		if err := common.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode codex local compaction ingress %s: %w", subject, err)
		}
		if relaycommon.ResponsesEncryptedContentValueIsOpaque(value) {
			reader.hasOpaqueEncryptedContent = true
		}
	default:
		reader.hasOpaqueEncryptedContent = true
	}
	return raw, nil
}

func (reader *codexLocalCompactionIngressJSONReader) copyArray(capture *codexLocalCompactionIngressJSONCapture, depth int) error {
	if err := reader.copySpace(capture); err != nil {
		return err
	}
	if closed, err := reader.consumeByte(capture, ']'); err != nil {
		return err
	} else if closed {
		return nil
	}
	for {
		first, err := reader.readByte(capture)
		if err != nil {
			return err
		}
		if err := reader.copyValueFromFirst(capture, first, depth); err != nil {
			return err
		}
		if err := reader.copySpace(capture); err != nil {
			return err
		}
		delimiter, err := reader.readByte(capture)
		if err != nil {
			return err
		}
		switch delimiter {
		case ']':
			return nil
		case ',':
			if err := reader.copySpace(capture); err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid JSON array delimiter in codex local compaction ingress request")
		}
	}
}

func (reader *codexLocalCompactionIngressJSONReader) copyStringTail(capture *codexLocalCompactionIngressJSONCapture) error {
	for {
		character, err := reader.readByte(capture)
		if err != nil {
			return err
		}
		switch character {
		case '"':
			return nil
		case '\\':
			escape, err := reader.readByte(capture)
			if err != nil {
				return err
			}
			switch escape {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				for range 4 {
					hex, err := reader.readByte(capture)
					if err != nil {
						return err
					}
					if !isJSONHex(hex) {
						return fmt.Errorf("invalid JSON Unicode escape in codex local compaction ingress request")
					}
				}
			default:
				return fmt.Errorf("invalid JSON escape in codex local compaction ingress request")
			}
		default:
			if character < 0x20 {
				return fmt.Errorf("invalid JSON control character in codex local compaction ingress request")
			}
		}
	}
}

func (reader *codexLocalCompactionIngressJSONReader) copyLiteralTail(capture *codexLocalCompactionIngressJSONCapture, tail string) error {
	for index := range len(tail) {
		character, err := reader.readByte(capture)
		if err != nil {
			return err
		}
		if character != tail[index] {
			return fmt.Errorf("invalid JSON literal in codex local compaction ingress request")
		}
	}
	return nil
}

func (reader *codexLocalCompactionIngressJSONReader) copyNumberTail(capture *codexLocalCompactionIngressJSONCapture, first byte) error {
	character := first
	if character == '-' {
		var err error
		character, err = reader.readByte(capture)
		if err != nil {
			return err
		}
	}
	if character == '0' {
		if next, ok, err := reader.peekByte(); err != nil {
			return err
		} else if ok && isJSONDigit(next) {
			return fmt.Errorf("invalid JSON number in codex local compaction ingress request")
		}
	} else if isJSONDigitOneToNine(character) {
		if err := reader.copyDigits(capture); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("invalid JSON number in codex local compaction ingress request")
	}
	if decimal, err := reader.consumeByte(capture, '.'); err != nil {
		return err
	} else if decimal {
		if err := reader.copyRequiredDigit(capture); err != nil {
			return err
		}
		if err := reader.copyDigits(capture); err != nil {
			return err
		}
	}
	if exponent, err := reader.consumeEitherByte(capture, 'e', 'E'); err != nil {
		return err
	} else if exponent {
		if _, err := reader.consumeEitherByte(capture, '+', '-'); err != nil {
			return err
		}
		if err := reader.copyRequiredDigit(capture); err != nil {
			return err
		}
		if err := reader.copyDigits(capture); err != nil {
			return err
		}
	}
	return nil
}

func (reader *codexLocalCompactionIngressJSONReader) copyRequiredDigit(capture *codexLocalCompactionIngressJSONCapture) error {
	character, err := reader.readByte(capture)
	if err != nil {
		return err
	}
	if !isJSONDigit(character) {
		return fmt.Errorf("invalid JSON number in codex local compaction ingress request")
	}
	return nil
}

func (reader *codexLocalCompactionIngressJSONReader) copyDigits(capture *codexLocalCompactionIngressJSONCapture) error {
	for {
		character, ok, err := reader.peekByte()
		if err != nil {
			return err
		}
		if !ok || !isJSONDigit(character) {
			return nil
		}
		if _, err := reader.readByte(capture); err != nil {
			return err
		}
	}
}

func (reader *codexLocalCompactionIngressJSONReader) expectByte(capture *codexLocalCompactionIngressJSONCapture, expected byte) error {
	actual, err := reader.readByte(capture)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("expected JSON %q in codex local compaction ingress request", expected)
	}
	return nil
}

func (reader *codexLocalCompactionIngressJSONReader) consumeByte(capture *codexLocalCompactionIngressJSONCapture, expected byte) (bool, error) {
	actual, ok, err := reader.peekByte()
	if err != nil || !ok || actual != expected {
		return false, err
	}
	_, err = reader.readByte(capture)
	return true, err
}

func (reader *codexLocalCompactionIngressJSONReader) consumeEitherByte(capture *codexLocalCompactionIngressJSONCapture, first, second byte) (bool, error) {
	actual, ok, err := reader.peekByte()
	if err != nil || !ok || (actual != first && actual != second) {
		return false, err
	}
	_, err = reader.readByte(capture)
	return true, err
}

func (reader *codexLocalCompactionIngressJSONReader) copySpace(capture *codexLocalCompactionIngressJSONCapture) error {
	for {
		character, ok, err := reader.peekByte()
		if err != nil {
			return err
		}
		if !ok || !isJSONSpace(character) {
			return nil
		}
		if _, err := reader.readByte(capture); err != nil {
			return err
		}
	}
}

func (reader *codexLocalCompactionIngressJSONReader) skipSpace() error {
	return reader.copySpace(nil)
}

func (reader *codexLocalCompactionIngressJSONReader) expectEnd() error {
	if err := reader.skipSpace(); err != nil {
		return err
	}
	_, ok, err := reader.peekByte()
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("codex local compaction ingress request contains trailing JSON data")
	}
	return nil
}

func (reader *codexLocalCompactionIngressJSONReader) readByte(capture *codexLocalCompactionIngressJSONCapture) (byte, error) {
	character, err := reader.reader.ReadByte()
	if err != nil {
		return 0, err
	}
	reader.bytesRead++
	if err := capture.writeByte(character); err != nil {
		return 0, err
	}
	return character, nil
}

func (reader *codexLocalCompactionIngressJSONReader) peekByte() (byte, bool, error) {
	data, err := reader.reader.Peek(1)
	if err == io.EOF {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return data[0], true, nil
}

func (capture *codexLocalCompactionIngressJSONCapture) writeByte(character byte) error {
	if capture == nil {
		return nil
	}
	if capture.max <= 0 {
		return fmt.Errorf("%w: %s exceeds %d bytes", ErrCodexLocalCompactionIngressValueTooLarge, capture.subject, capture.max)
	}
	if capture.data.Len() >= capture.max {
		return fmt.Errorf("%w: %s exceeds %d bytes", ErrCodexLocalCompactionIngressValueTooLarge, capture.subject, capture.max)
	}
	return capture.data.WriteByte(character)
}

func (capture *codexLocalCompactionIngressJSONCapture) writeBytes(data []byte) error {
	for _, character := range data {
		if err := capture.writeByte(character); err != nil {
			return err
		}
	}
	return nil
}

func (capture *codexLocalCompactionIngressJSONCapture) objectFieldValueMaxBytes(encodedField []byte) (int, error) {
	reserved := len(encodedField) + 2 // JSON field name, colon, and closing brace.
	if capture.data.Len() > 1 {
		reserved++ // Comma before every retained field after the first.
	}
	available := capture.max - capture.data.Len() - reserved
	if available <= 0 {
		return 0, fmt.Errorf("%w: %s exceeds %d bytes", ErrCodexLocalCompactionIngressValueTooLarge, capture.subject, capture.max)
	}
	return available, nil
}

func (capture *codexLocalCompactionIngressJSONCapture) appendObjectField(encodedField []byte, rawValue common.RawMessage) error {
	if capture.data.Len() > 1 {
		if err := capture.writeByte(','); err != nil {
			return err
		}
	}
	if err := capture.writeBytes(encodedField); err != nil {
		return err
	}
	if err := capture.writeByte(':'); err != nil {
		return err
	}
	return capture.writeBytes(rawValue)
}

func isJSONSpace(character byte) bool {
	switch character {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

func codexLocalCompactionIngressJSONValueType(first byte) string {
	switch first {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return "number"
	default:
		return "unknown"
	}
}
