package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

type RawMessage = json.RawMessage
type JsonDecoder = json.Decoder
type JsonDelim = json.Delim
type JsonNumber = json.Number

func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func UnmarshalJsonStr(data string, v any) error {
	return json.Unmarshal(StringToByteSlice(data), v)
}

func DecodeJson(reader io.Reader, v any) error {
	return json.NewDecoder(reader).Decode(v)
}

func DecodeJsonUseNumber(reader io.Reader, v any) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	return decoder.Decode(v)
}

func NewJsonDecoderUseNumber(reader io.Reader) *JsonDecoder {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	return decoder
}

func DecodeJsonFromDecoder(decoder *JsonDecoder, v any) error {
	return decoder.Decode(v)
}

func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func ValidJson(data []byte) bool {
	return json.Valid(data)
}

func UnmarshalWithNumber(data []byte, v any) error {
	return DecodeJsonUseNumber(bytes.NewReader(data), v)
}

// sanitizeJSONKindName maps a Go reflect.Kind onto the JSON kind name a client
// can reason about. Every kind maps onto one of the JSON names, so a sanitized
// message never carries a Go type name (which would leak internal struct
// definitions to API consumers).
func sanitizeJSONKindName(k reflect.Kind) string {
	switch k {
	case reflect.Struct, reflect.Map:
		return "object"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	default:
		return "value"
	}
}

// SanitizeJSONDecodeError rewrites a raw *json.UnmarshalTypeError into a
// client-safe message. The raw error names the gateway's internal Go struct
// and field types ("cannot unmarshal string into Go struct field
// probeRequest.max_tokens of type uint"); the sanitized form names only the
// JSON field and the JSON kind it must be. Non-type errors carry no Go type
// information and are returned unchanged so their established wording is
// preserved.
func SanitizeJSONDecodeError(err error) error {
	if err == nil {
		return nil
	}
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	field := typeErr.Field
	if field == "" {
		field = "the request body"
	}
	return fmt.Errorf("json: cannot unmarshal %s into field %s: field %s must be a %s",
		sanitizeJSONKindName(typeErr.Type.Kind()), field, field, sanitizeJSONKindName(typeErr.Type.Kind()))
}

func GetJsonType(data json.RawMessage) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "unknown"
	}
	firstChar := trimmed[0]
	switch firstChar {
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
	default:
		return "number"
	}
}

// JsonRawMessageToString returns JSON strings as their decoded value and other JSON values as raw text.
func JsonRawMessageToString(data json.RawMessage) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] != '"' {
		return string(trimmed)
	}
	var value string
	if err := Unmarshal(trimmed, &value); err != nil {
		return string(trimmed)
	}
	return value
}
