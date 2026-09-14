package service

import (
	"bytes"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

type responsesJSONScanner struct {
	data []byte
}

func visitResponsesJSONArray(raw common.RawMessage, visitor func(common.RawMessage) bool) bool {
	scanner := responsesJSONScanner{data: bytes.TrimSpace(raw)}
	end, ok := scanner.visitArray(0, 0, visitor)
	return ok && end == len(scanner.data)
}

func visitResponsesJSONObject(raw common.RawMessage, visitor func(string, common.RawMessage) bool) bool {
	scanner := responsesJSONScanner{data: bytes.TrimSpace(raw)}
	end, ok := scanner.visitObject(0, 0, visitor)
	return ok && end == len(scanner.data)
}

func (scanner responsesJSONScanner) visitArray(index, depth int, visitor func(common.RawMessage) bool) (int, bool) {
	if depth > syntheticCompactJSONNestingDepthMax || index >= len(scanner.data) || scanner.data[index] != '[' {
		return 0, false
	}
	index = scanner.skipSpace(index + 1)
	if index < len(scanner.data) && scanner.data[index] == ']' {
		return index + 1, true
	}
	for {
		start := index
		end, ok := scanner.scanValue(index, depth+1)
		if !ok {
			return 0, false
		}
		if visitor != nil && !visitor(scanner.data[start:end]) {
			return 0, false
		}
		index = scanner.skipSpace(end)
		if index >= len(scanner.data) {
			return 0, false
		}
		switch scanner.data[index] {
		case ',':
			index = scanner.skipSpace(index + 1)
			if index >= len(scanner.data) {
				return 0, false
			}
		case ']':
			return index + 1, true
		default:
			return 0, false
		}
	}
}

func (scanner responsesJSONScanner) visitObject(index, depth int, visitor func(string, common.RawMessage) bool) (int, bool) {
	if depth > syntheticCompactJSONNestingDepthMax || index >= len(scanner.data) || scanner.data[index] != '{' {
		return 0, false
	}
	index = scanner.skipSpace(index + 1)
	if index < len(scanner.data) && scanner.data[index] == '}' {
		return index + 1, true
	}
	for {
		keyStart := index
		keyEnd, ok := scanner.scanString(index)
		if !ok {
			return 0, false
		}
		key := rawStringField(scanner.data[keyStart:keyEnd])
		index = scanner.skipSpace(keyEnd)
		if index >= len(scanner.data) || scanner.data[index] != ':' {
			return 0, false
		}
		index = scanner.skipSpace(index + 1)
		valueStart := index
		valueEnd, ok := scanner.scanValue(index, depth+1)
		if !ok {
			return 0, false
		}
		if visitor != nil && !visitor(key, scanner.data[valueStart:valueEnd]) {
			return 0, false
		}
		index = scanner.skipSpace(valueEnd)
		if index >= len(scanner.data) {
			return 0, false
		}
		switch scanner.data[index] {
		case ',':
			index = scanner.skipSpace(index + 1)
			if index >= len(scanner.data) {
				return 0, false
			}
		case '}':
			return index + 1, true
		default:
			return 0, false
		}
	}
}

func (scanner responsesJSONScanner) scanValue(index, depth int) (int, bool) {
	if depth > syntheticCompactJSONNestingDepthMax {
		return 0, false
	}
	index = scanner.skipSpace(index)
	if index >= len(scanner.data) {
		return 0, false
	}
	switch scanner.data[index] {
	case '"':
		return scanner.scanString(index)
	case '[':
		return scanner.visitArray(index, depth, nil)
	case '{':
		return scanner.visitObject(index, depth, nil)
	default:
		return scanner.scanPrimitive(index)
	}
}

func (scanner responsesJSONScanner) scanString(index int) (int, bool) {
	if index >= len(scanner.data) || scanner.data[index] != '"' {
		return 0, false
	}
	for index++; index < len(scanner.data); {
		character := scanner.data[index]
		switch character {
		case '"':
			return index + 1, true
		case '\\':
			index++
			if index >= len(scanner.data) {
				return 0, false
			}
			switch scanner.data[index] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				index++
			case 'u':
				if index+4 >= len(scanner.data) ||
					!isJSONHex(scanner.data[index+1]) ||
					!isJSONHex(scanner.data[index+2]) ||
					!isJSONHex(scanner.data[index+3]) ||
					!isJSONHex(scanner.data[index+4]) {
					return 0, false
				}
				index += 5
			default:
				return 0, false
			}
		default:
			if character < 0x20 {
				return 0, false
			}
			if character < utf8.RuneSelf {
				index++
				continue
			}
			_, size := utf8.DecodeRune(scanner.data[index:])
			if size == 1 {
				return 0, false
			}
			index += size
		}
	}
	return 0, false
}

func (scanner responsesJSONScanner) scanPrimitive(index int) (int, bool) {
	if bytes.HasPrefix(scanner.data[index:], []byte("true")) {
		return index + len("true"), true
	}
	if bytes.HasPrefix(scanner.data[index:], []byte("false")) {
		return index + len("false"), true
	}
	if bytes.HasPrefix(scanner.data[index:], []byte("null")) {
		return index + len("null"), true
	}
	start := index
	if scanner.data[index] == '-' {
		index++
	}
	if index >= len(scanner.data) {
		return 0, false
	}
	if scanner.data[index] == '0' {
		index++
	} else if isJSONDigitOneToNine(scanner.data[index]) {
		for index < len(scanner.data) && isJSONDigit(scanner.data[index]) {
			index++
		}
	} else {
		return 0, false
	}
	if index < len(scanner.data) && scanner.data[index] == '.' {
		index++
		fractionStart := index
		for index < len(scanner.data) && isJSONDigit(scanner.data[index]) {
			index++
		}
		if index == fractionStart {
			return 0, false
		}
	}
	if index < len(scanner.data) && (scanner.data[index] == 'e' || scanner.data[index] == 'E') {
		index++
		if index < len(scanner.data) && (scanner.data[index] == '+' || scanner.data[index] == '-') {
			index++
		}
		exponentStart := index
		for index < len(scanner.data) && isJSONDigit(scanner.data[index]) {
			index++
		}
		if index == exponentStart {
			return 0, false
		}
	}
	if index == start {
		return 0, false
	}
	return index, true
}

func (scanner responsesJSONScanner) skipSpace(index int) int {
	for index < len(scanner.data) {
		switch scanner.data[index] {
		case ' ', '\t', '\r', '\n':
			index++
		default:
			return index
		}
	}
	return index
}

func isJSONHex(character byte) bool {
	return (character >= '0' && character <= '9') ||
		(character >= 'a' && character <= 'f') ||
		(character >= 'A' && character <= 'F')
}

func isJSONDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

func isJSONDigitOneToNine(character byte) bool {
	return character >= '1' && character <= '9'
}
