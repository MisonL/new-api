package common

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"hash"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The opaque replay scanner streams encrypted-content strings without retaining request content.
type responsesEncryptedContentSummary struct {
	hasOpaqueContent bool
	fingerprint      string
}

func (summary *responsesEncryptedContentSummary) merge(other responsesEncryptedContentSummary) {
	if summary == nil || !other.hasOpaqueContent {
		return
	}
	summary.hasOpaqueContent = true
	if summary.fingerprint == "" {
		summary.fingerprint = other.fingerprint
	}
}

func (s *jsonTopLevelScanner) readResponsesEncryptedContent(first byte, depth int) (responsesEncryptedContentSummary, error) {
	if first == 'n' {
		if err := s.expectLiteral("ull"); err != nil {
			return responsesEncryptedContentSummary{}, err
		}
		return responsesEncryptedContentSummary{}, nil
	}
	if first != '"' {
		if err := s.skipValue(first, depth); err != nil {
			return responsesEncryptedContentSummary{}, err
		}
		return responsesEncryptedContentSummary{hasOpaqueContent: true}, nil
	}
	summary, err := s.readJSONStringSummary(maxTopLevelJSONStringCaptureBytes)
	if err != nil {
		return responsesEncryptedContentSummary{}, err
	}
	return responsesEncryptedContentSummary{
		hasOpaqueContent: summary.hasContent,
		fingerprint:      summary.fingerprint,
	}, nil
}

type jsonStringSummary struct {
	hasContent  bool
	fingerprint string
}

func (s *jsonTopLevelScanner) readJSONStringSummary(captureLimit int) (jsonStringSummary, error) {
	var raw []byte
	captured := captureLimit > 0
	if captured {
		raw = append(raw, '"')
	}
	trimmer := newResponsesMetadataStringTrimmer()
	appendRaw := func(value byte) {
		if !captured {
			return
		}
		if len(raw) >= captureLimit+2 {
			captured = false
			raw = nil
			return
		}
		raw = append(raw, value)
	}
	read := func() (byte, error) {
		value, err := s.readByte()
		if err == nil {
			appendRaw(value)
		}
		return value, err
	}

	for {
		value, err := read()
		if err != nil {
			return jsonStringSummary{}, fmt.Errorf("read JSON string: %w", err)
		}
		if value < 0x20 {
			return jsonStringSummary{}, errors.New("JSON string contains control character")
		}
		if value == '"' {
			break
		}
		if value != '\\' {
			trimmer.AppendByte(value)
			continue
		}

		escape, err := read()
		if err != nil {
			return jsonStringSummary{}, fmt.Errorf("read JSON string escape: %w", err)
		}
		switch escape {
		case '"', '\\', '/':
			trimmer.AppendByte(escape)
		case 'b':
			trimmer.AppendByte('\b')
		case 'f':
			trimmer.AppendByte('\f')
		case 'n':
			trimmer.AppendByte('\n')
		case 'r':
			trimmer.AppendByte('\r')
		case 't':
			trimmer.AppendByte('\t')
		case 'u':
			runeValue, err := readResponsesMetadataUnicodeEscape(read)
			if err != nil {
				return jsonStringSummary{}, err
			}
			if runeValue >= 0xD800 && runeValue <= 0xDBFF {
				if tail, err := s.reader.Peek(6); err == nil && len(tail) == 6 && tail[0] == '\\' && tail[1] == 'u' {
					low, valid := parseResponsesMetadataUnicodeHex(tail[2:])
					if valid && low >= 0xDC00 && low <= 0xDFFF {
						for range 6 {
							if _, err := read(); err != nil {
								return jsonStringSummary{}, fmt.Errorf("read JSON surrogate escape: %w", err)
							}
						}
						runeValue = 0x10000 + ((runeValue - 0xD800) << 10) + (low - 0xDC00)
					}
				}
			}
			trimmer.AppendRune(rune(runeValue))
		default:
			return jsonStringSummary{}, errors.New("invalid JSON string escape")
		}
	}

	trimmer.Flush()
	if captured {
		var value string
		if err := Unmarshal(raw, &value); err != nil {
			return jsonStringSummary{}, fmt.Errorf("decode JSON string: %w", err)
		}
		trimmed := strings.TrimSpace(value)
		return jsonStringSummary{
			hasContent:  trimmed != "",
			fingerprint: responsesMetadataFingerprint(trimmed),
		}, nil
	}
	return jsonStringSummary{
		hasContent:  trimmer.HasContent(),
		fingerprint: trimmer.Fingerprint(),
	}, nil
}

func readResponsesMetadataUnicodeEscape(read func() (byte, error)) (uint32, error) {
	var raw [4]byte
	for index := range raw {
		value, err := read()
		if err != nil {
			return 0, fmt.Errorf("read JSON unicode escape: %w", err)
		}
		if !isJSONHex(value) {
			return 0, errors.New("invalid JSON unicode escape")
		}
		raw[index] = value
	}
	value, ok := parseResponsesMetadataUnicodeHex(raw[:])
	if !ok {
		return 0, errors.New("invalid JSON unicode escape")
	}
	return value, nil
}

func parseResponsesMetadataUnicodeHex(raw []byte) (uint32, bool) {
	if len(raw) != 4 {
		return 0, false
	}
	var value uint32
	for _, character := range raw {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += uint32(character - '0')
		case character >= 'a' && character <= 'f':
			value += uint32(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += uint32(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

type responsesMetadataStringTrimmer struct {
	hash                   hash.Hash
	utf8Pending            []byte
	pendingWhitespace      []byte
	hasContent             bool
	fingerprintUnavailable bool
}

func newResponsesMetadataStringTrimmer() *responsesMetadataStringTrimmer {
	return &responsesMetadataStringTrimmer{hash: sha1.New()}
}

func (t *responsesMetadataStringTrimmer) AppendByte(value byte) {
	t.Append([]byte{value})
}

func (t *responsesMetadataStringTrimmer) AppendRune(value rune) {
	var raw [utf8.UTFMax]byte
	size := utf8.EncodeRune(raw[:], value)
	t.Append(raw[:size])
}

func (t *responsesMetadataStringTrimmer) Append(value []byte) {
	if t == nil || len(value) == 0 {
		return
	}
	t.utf8Pending = append(t.utf8Pending, value...)
	for len(t.utf8Pending) > 0 {
		runeValue, size := utf8.DecodeRune(t.utf8Pending)
		if runeValue == utf8.RuneError && size == 1 && !utf8.FullRune(t.utf8Pending) {
			return
		}
		t.writeRuneBytes(runeValue, t.utf8Pending[:size])
		t.utf8Pending = t.utf8Pending[size:]
	}
}

func (t *responsesMetadataStringTrimmer) Flush() {
	if t == nil || len(t.utf8Pending) == 0 {
		return
	}
	for len(t.utf8Pending) > 0 {
		t.writeRuneBytes(utf8.RuneError, t.utf8Pending[:1])
		t.utf8Pending = t.utf8Pending[1:]
	}
}

func (t *responsesMetadataStringTrimmer) writeRuneBytes(runeValue rune, raw []byte) {
	if !t.hasContent && unicode.IsSpace(runeValue) {
		return
	}
	if unicode.IsSpace(runeValue) {
		if t.fingerprintUnavailable {
			return
		}
		if len(t.pendingWhitespace)+len(raw) > maxResponsesMetadataTrailingWhitespaceBytes {
			t.pendingWhitespace = nil
			t.fingerprintUnavailable = true
			return
		}
		t.pendingWhitespace = append(t.pendingWhitespace, raw...)
		return
	}
	if !t.hasContent {
		t.hasContent = true
	}
	if !t.fingerprintUnavailable && len(t.pendingWhitespace) > 0 {
		_, _ = t.hash.Write(t.pendingWhitespace)
		t.pendingWhitespace = nil
	}
	if !t.fingerprintUnavailable {
		_, _ = t.hash.Write(raw)
	}
}

func (t *responsesMetadataStringTrimmer) HasContent() bool {
	return t != nil && t.hasContent
}

func (t *responsesMetadataStringTrimmer) Fingerprint() string {
	if t == nil || !t.hasContent || t.fingerprintUnavailable {
		return ""
	}
	return fmt.Sprintf("%x", t.hash.Sum(nil))[:8]
}

func responsesMetadataFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return Sha1([]byte(value))[:8]
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
