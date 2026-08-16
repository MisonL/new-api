package common

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

const maxTopLevelJSONStringCaptureBytes = 64 * 1024

var ErrTopLevelJSONStringNotFound = errors.New("top-level JSON string field not found")

// TopLevelJSONStringLocation identifies the complete JSON string token for a
// field in a top-level JSON object. End is exclusive.
type TopLevelJSONStringLocation struct {
	Start int64
	End   int64
	Value string
}

// LocateTopLevelJSONString scans a JSON object without materializing nested
// values and returns the final occurrence of a top-level string field.
func LocateTopLevelJSONString(source io.ReadSeeker, field string) (TopLevelJSONStringLocation, error) {
	if source == nil {
		return TopLevelJSONStringLocation{}, errors.New("JSON source is required")
	}
	field = strings.TrimSpace(field)
	if field == "" {
		return TopLevelJSONStringLocation{}, errors.New("JSON field is required")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return TopLevelJSONStringLocation{}, fmt.Errorf("reset JSON source: %w", err)
	}
	defer func() {
		_, _ = source.Seek(0, io.SeekStart)
	}()

	scanner := newJSONTopLevelScanner(source)
	first, err := scanner.nextNonWhitespace()
	if err != nil {
		return TopLevelJSONStringLocation{}, fmt.Errorf("read JSON root: %w", err)
	}
	if first != '{' {
		return TopLevelJSONStringLocation{}, errors.New("JSON root must be an object")
	}

	var location TopLevelJSONStringLocation
	found := false
	lastTargetWasNonString := false
	afterComma := false
	for {
		next, err := scanner.nextNonWhitespace()
		if err != nil {
			return TopLevelJSONStringLocation{}, fmt.Errorf("read JSON object field: %w", err)
		}
		if next == '}' {
			if afterComma {
				return TopLevelJSONStringLocation{}, errors.New("JSON object cannot end after comma")
			}
			break
		}
		if next != '"' {
			return TopLevelJSONStringLocation{}, errors.New("JSON object field name must be a string")
		}
		afterComma = false
		key, captured, err := scanner.readJSONString(maxTopLevelJSONStringCaptureBytes)
		if err != nil {
			return TopLevelJSONStringLocation{}, fmt.Errorf("read JSON object field name: %w", err)
		}
		next, err = scanner.nextNonWhitespace()
		if err != nil {
			return TopLevelJSONStringLocation{}, fmt.Errorf("read JSON object separator: %w", err)
		}
		if next != ':' {
			return TopLevelJSONStringLocation{}, errors.New("JSON object field is missing colon")
		}
		value, err := scanner.nextNonWhitespace()
		if err != nil {
			return TopLevelJSONStringLocation{}, fmt.Errorf("read JSON object value: %w", err)
		}

		if captured && key == field {
			if value != '"' {
				if err := scanner.skipValue(value, 1); err != nil {
					return TopLevelJSONStringLocation{}, fmt.Errorf("skip top-level JSON field %q: %w", field, err)
				}
				found = false
				lastTargetWasNonString = true
			} else {
				start := scanner.offset - 1
				stringValue, valueCaptured, err := scanner.readJSONString(maxTopLevelJSONStringCaptureBytes)
				if err != nil {
					return TopLevelJSONStringLocation{}, fmt.Errorf("read top-level JSON field %q: %w", field, err)
				}
				if !valueCaptured {
					return TopLevelJSONStringLocation{}, fmt.Errorf("top-level JSON field %q exceeds %d bytes", field, maxTopLevelJSONStringCaptureBytes)
				}
				location = TopLevelJSONStringLocation{
					Start: start,
					End:   scanner.offset,
					Value: stringValue,
				}
				found = true
				lastTargetWasNonString = false
			}
		} else if err := scanner.skipValue(value, 1); err != nil {
			return TopLevelJSONStringLocation{}, fmt.Errorf("skip JSON object field value: %w", err)
		}

		next, err = scanner.nextNonWhitespace()
		if err != nil {
			return TopLevelJSONStringLocation{}, fmt.Errorf("read JSON object delimiter: %w", err)
		}
		switch next {
		case ',':
			afterComma = true
			continue
		case '}':
			goto done
		default:
			return TopLevelJSONStringLocation{}, errors.New("JSON object field is missing comma")
		}
	}

done:
	if err := scanner.expectEOF(); err != nil {
		return TopLevelJSONStringLocation{}, err
	}
	if lastTargetWasNonString {
		return TopLevelJSONStringLocation{}, fmt.Errorf("top-level JSON field %q must be a string", field)
	}
	if !found {
		return TopLevelJSONStringLocation{}, fmt.Errorf("%w: %s", ErrTopLevelJSONStringNotFound, field)
	}
	return location, nil
}

// SizedReader bounds a reader to an explicit byte size and exposes that size to
// relay callers that need a deterministic upstream Content-Length.
type SizedReader struct {
	source    io.Reader
	remaining int64
	size      int64
}

const maxReplayReaderEmptyReads = 100

func NewSizedReader(source io.ReadSeeker, size int64) (*SizedReader, error) {
	if source == nil {
		return nil, errors.New("sized reader source is required")
	}
	if size < 0 {
		return nil, errors.New("sized reader size must not be negative")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("reset sized reader source: %w", err)
	}
	return &SizedReader{source: source, remaining: size, size: size}, nil
}

func (r *SizedReader) Read(p []byte) (int, error) {
	if r == nil || r.remaining <= 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := readReplaySource(r.source, p)
	if n > 0 {
		r.remaining -= int64(n)
		return n, nil
	}
	if err == io.EOF {
		return 0, io.ErrUnexpectedEOF
	}
	return n, err
}

func (r *SizedReader) ContentLength() int64 {
	if r == nil {
		return -1
	}
	return r.size
}

// TopLevelJSONStringReplacementReader replays a source body while replacing
// exactly one complete top-level JSON string token. It never buffers the body.
type TopLevelJSONStringReplacementReader struct {
	source          io.ReadSeeker
	location        TopLevelJSONStringLocation
	replacement     []byte
	prefixRemaining int64
	suffixRemaining int64
	replacementPos  int
	phase           topLevelJSONStringReplayPhase
	contentLength   int64
}

type topLevelJSONStringReplayPhase uint8

const (
	topLevelJSONStringReplayPrefix topLevelJSONStringReplayPhase = iota
	topLevelJSONStringReplayReplacement
	topLevelJSONStringReplaySuffix
	topLevelJSONStringReplayDone
)

func NewTopLevelJSONStringReplacementReader(source io.ReadSeeker, sourceSize int64, location TopLevelJSONStringLocation, replacement string) (*TopLevelJSONStringReplacementReader, error) {
	if source == nil {
		return nil, errors.New("JSON replacement source is required")
	}
	if sourceSize < 0 {
		return nil, errors.New("JSON replacement source size must not be negative")
	}
	if location.Start < 0 || location.End <= location.Start || location.End > sourceSize {
		return nil, errors.New("invalid top-level JSON string location")
	}
	replacementJSON, err := Marshal(replacement)
	if err != nil {
		return nil, fmt.Errorf("encode replacement JSON string: %w", err)
	}
	originalSize := location.End - location.Start
	if int64(len(replacementJSON)) > math.MaxInt64-(sourceSize-originalSize) {
		return nil, errors.New("replacement JSON body size overflows int64")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("reset JSON replacement source: %w", err)
	}
	return &TopLevelJSONStringReplacementReader{
		source:          source,
		location:        location,
		replacement:     replacementJSON,
		prefixRemaining: location.Start,
		suffixRemaining: sourceSize - location.End,
		phase:           topLevelJSONStringReplayPrefix,
		contentLength:   sourceSize - originalSize + int64(len(replacementJSON)),
	}, nil
}

func (r *TopLevelJSONStringReplacementReader) Read(p []byte) (int, error) {
	if r == nil || r.phase == topLevelJSONStringReplayDone {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}

	for {
		switch r.phase {
		case topLevelJSONStringReplayPrefix:
			if r.prefixRemaining == 0 {
				if _, err := r.source.Seek(r.location.End, io.SeekStart); err != nil {
					return 0, fmt.Errorf("seek JSON replacement suffix: %w", err)
				}
				r.phase = topLevelJSONStringReplayReplacement
				continue
			}
			n, err := readBounded(r.source, p, &r.prefixRemaining)
			if n > 0 {
				return n, nil
			}
			return 0, err
		case topLevelJSONStringReplayReplacement:
			if r.replacementPos >= len(r.replacement) {
				r.phase = topLevelJSONStringReplaySuffix
				continue
			}
			n := copy(p, r.replacement[r.replacementPos:])
			r.replacementPos += n
			return n, nil
		case topLevelJSONStringReplaySuffix:
			if r.suffixRemaining == 0 {
				r.phase = topLevelJSONStringReplayDone
				continue
			}
			n, err := readBounded(r.source, p, &r.suffixRemaining)
			if n > 0 {
				return n, nil
			}
			return 0, err
		default:
			return 0, io.EOF
		}
	}
}

func (r *TopLevelJSONStringReplacementReader) ContentLength() int64 {
	if r == nil {
		return -1
	}
	return r.contentLength
}

func readBounded(source io.Reader, target []byte, remaining *int64) (int, error) {
	if remaining == nil || *remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(target)) > *remaining {
		target = target[:*remaining]
	}
	n, err := readReplaySource(source, target)
	if n > 0 {
		*remaining -= int64(n)
		return n, nil
	}
	if err == io.EOF {
		return 0, io.ErrUnexpectedEOF
	}
	return n, err
}

func readReplaySource(source io.Reader, target []byte) (int, error) {
	for attempts := 0; attempts < maxReplayReaderEmptyReads; attempts++ {
		n, err := source.Read(target)
		if n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}

type jsonTopLevelScanner struct {
	reader *bufio.Reader
	offset int64
}

func newJSONTopLevelScanner(source io.Reader) *jsonTopLevelScanner {
	return &jsonTopLevelScanner{reader: bufio.NewReaderSize(source, 64*1024)}
}

func (s *jsonTopLevelScanner) readByte() (byte, error) {
	b, err := s.reader.ReadByte()
	if err == nil {
		s.offset++
	}
	return b, err
}

func (s *jsonTopLevelScanner) peekByte() (byte, error) {
	value, err := s.reader.Peek(1)
	if err != nil {
		return 0, err
	}
	return value[0], nil
}

func (s *jsonTopLevelScanner) nextNonWhitespace() (byte, error) {
	for {
		b, err := s.readByte()
		if err != nil {
			return 0, err
		}
		if !isJSONWhitespace(b) {
			return b, nil
		}
	}
}

func (s *jsonTopLevelScanner) expectEOF() error {
	for {
		b, err := s.readByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read trailing JSON content: %w", err)
		}
		if !isJSONWhitespace(b) {
			return errors.New("JSON contains trailing content")
		}
	}
}

func (s *jsonTopLevelScanner) readJSONString(captureLimit int) (string, bool, error) {
	var raw []byte
	captured := captureLimit > 0
	if captured {
		raw = append(raw, '"')
	}
	for {
		b, err := s.readByte()
		if err != nil {
			return "", false, fmt.Errorf("read JSON string: %w", err)
		}
		if b < 0x20 {
			return "", false, errors.New("JSON string contains control character")
		}
		if captured {
			if len(raw) >= captureLimit+2 {
				captured = false
				raw = nil
			} else {
				raw = append(raw, b)
			}
		}
		if b == '"' {
			break
		}
		if b != '\\' {
			continue
		}
		escape, err := s.readByte()
		if err != nil {
			return "", false, fmt.Errorf("read JSON string escape: %w", err)
		}
		if captured {
			if len(raw) >= captureLimit+2 {
				captured = false
				raw = nil
			} else {
				raw = append(raw, escape)
			}
		}
		switch escape {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		case 'u':
			for index := 0; index < 4; index++ {
				hex, err := s.readByte()
				if err != nil {
					return "", false, fmt.Errorf("read JSON unicode escape: %w", err)
				}
				if !isJSONHex(hex) {
					return "", false, errors.New("invalid JSON unicode escape")
				}
				if captured {
					if len(raw) >= captureLimit+2 {
						captured = false
						raw = nil
					} else {
						raw = append(raw, hex)
					}
				}
			}
		default:
			return "", false, errors.New("invalid JSON string escape")
		}
	}
	if !captured {
		return "", false, nil
	}
	var value string
	if err := Unmarshal(raw, &value); err != nil {
		return "", false, fmt.Errorf("decode JSON string: %w", err)
	}
	return value, true, nil
}

func (s *jsonTopLevelScanner) skipValue(first byte, depth int) error {
	if depth > 1000 {
		return errors.New("JSON nesting depth exceeds 1000")
	}
	switch first {
	case '{':
		return s.skipObject(depth + 1)
	case '[':
		return s.skipArray(depth + 1)
	case '"':
		_, _, err := s.readJSONString(0)
		return err
	case 't':
		return s.expectLiteral("rue")
	case 'f':
		return s.expectLiteral("alse")
	case 'n':
		return s.expectLiteral("ull")
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return s.skipNumber(first)
	default:
		return errors.New("invalid JSON value")
	}
}

func (s *jsonTopLevelScanner) skipObject(depth int) error {
	next, err := s.nextNonWhitespace()
	if err != nil {
		return err
	}
	if next == '}' {
		return nil
	}
	for {
		if next != '"' {
			return errors.New("JSON object field name must be a string")
		}
		if _, _, err := s.readJSONString(0); err != nil {
			return err
		}
		next, err = s.nextNonWhitespace()
		if err != nil {
			return err
		}
		if next != ':' {
			return errors.New("JSON object field is missing colon")
		}
		next, err = s.nextNonWhitespace()
		if err != nil {
			return err
		}
		if err := s.skipValue(next, depth); err != nil {
			return err
		}
		next, err = s.nextNonWhitespace()
		if err != nil {
			return err
		}
		switch next {
		case ',':
			next, err = s.nextNonWhitespace()
			if err != nil {
				return err
			}
			if next == '}' {
				return errors.New("JSON object cannot end after comma")
			}
		case '}':
			return nil
		default:
			return errors.New("JSON object field is missing comma")
		}
	}
}

func (s *jsonTopLevelScanner) skipArray(depth int) error {
	next, err := s.nextNonWhitespace()
	if err != nil {
		return err
	}
	if next == ']' {
		return nil
	}
	for {
		if err := s.skipValue(next, depth); err != nil {
			return err
		}
		next, err = s.nextNonWhitespace()
		if err != nil {
			return err
		}
		switch next {
		case ',':
			next, err = s.nextNonWhitespace()
			if err != nil {
				return err
			}
		case ']':
			return nil
		default:
			return errors.New("JSON array value is missing comma")
		}
	}
}

func (s *jsonTopLevelScanner) expectLiteral(suffix string) error {
	for _, expected := range suffix {
		actual, err := s.readByte()
		if err != nil {
			return err
		}
		if actual != byte(expected) {
			return errors.New("invalid JSON literal")
		}
	}
	return nil
}

func (s *jsonTopLevelScanner) skipNumber(first byte) error {
	if first == '-' {
		next, err := s.readByte()
		if err != nil {
			return err
		}
		if next < '0' || next > '9' {
			return errors.New("invalid JSON number")
		}
		first = next
	}
	if first == '0' {
		next, err := s.peekByte()
		if err == nil && next >= '0' && next <= '9' {
			return errors.New("invalid JSON number leading zero")
		}
	} else {
		for {
			next, err := s.peekByte()
			if err != nil || next < '0' || next > '9' {
				break
			}
			_, _ = s.readByte()
		}
	}
	if next, err := s.peekByte(); err == nil && next == '.' {
		_, _ = s.readByte()
		digit, err := s.readByte()
		if err != nil {
			return err
		}
		if digit < '0' || digit > '9' {
			return errors.New("invalid JSON number fraction")
		}
		for {
			next, err := s.peekByte()
			if err != nil || next < '0' || next > '9' {
				break
			}
			_, _ = s.readByte()
		}
	}
	if next, err := s.peekByte(); err == nil && (next == 'e' || next == 'E') {
		_, _ = s.readByte()
		if sign, err := s.peekByte(); err == nil && (sign == '+' || sign == '-') {
			_, _ = s.readByte()
		}
		digit, err := s.readByte()
		if err != nil {
			return err
		}
		if digit < '0' || digit > '9' {
			return errors.New("invalid JSON number exponent")
		}
		for {
			next, err := s.peekByte()
			if err != nil || next < '0' || next > '9' {
				break
			}
			_, _ = s.readByte()
		}
	}
	if next, err := s.peekByte(); err == nil && !isJSONValueDelimiter(next) {
		return errors.New("invalid JSON number terminator")
	}
	return nil
}

func isJSONWhitespace(value byte) bool {
	return value == ' ' || value == '\n' || value == '\r' || value == '\t'
}

func isJSONHex(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f') || (value >= 'A' && value <= 'F')
}

func isJSONValueDelimiter(value byte) bool {
	return isJSONWhitespace(value) || value == ',' || value == ']' || value == '}'
}
