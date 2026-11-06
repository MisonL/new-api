package common

import (
	"bytes"
	"encoding/json"
	jsontext "encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// jsonV2UnmarshalOptions 让 v2 解码引擎保持 v1 的宽容语义。三者缺一不可：
//   - AllowInvalidUTF8：上游内容偶尔带非法 UTF-8，v1 用替换字符继续解码，
//     v2 默认整条报错，会把原本可用的响应或请求变成失败。
//   - MatchCaseInsensitiveNames：v1 字段匹配忽略大小写，客户端发 "Model"
//     也能命中 json:"model"，v2 默认大小写敏感，会静默丢字段。
//   - AllowDuplicateNames：v1 重复键后者生效，v2 默认报错。
//
// 有意不启用的 v1 兼容项（repo 内未使用或影响可接受）：非零目标值的合并
// 语义、宽松 RFC3339 时间、定长数组任意长度、",string" 标签、字节数组旧格式。
// 这些差异局限在"解码进已有非零值/罕见类型"的场景，常规请求体解码不受影响。
var jsonV2UnmarshalOptions = []jsonv2.Options{
	jsontext.AllowInvalidUTF8(true),
	jsonv2.MatchCaseInsensitiveNames(true),
	jsontext.AllowDuplicateNames(true),
}

type RawMessage = json.RawMessage
type JsonDecoder = json.Decoder
type JsonDelim = json.Delim
type JsonNumber = json.Number

func Unmarshal(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v, jsonV2UnmarshalOptions...)
}

func UnmarshalJsonStr(data string, v any) error {
	return jsonv2.Unmarshal(StringToByteSlice(data), v, jsonV2UnmarshalOptions...)
}

// DecodeJson 读取流中的第一个 JSON 值，保持 v1 Decoder.Decode 的语义：
// 顶层值之后的内容不消费、不报错，空输入返回 io.EOF。不能直接用
// jsonv2.UnmarshalRead——它会读到 EOF 并对尾随内容报错，对上游响应体
// 和多值流都会改变行为。
func DecodeJson(reader io.Reader, v any) error {
	return jsonv2.UnmarshalDecode(jsontext.NewDecoder(reader), v, jsonV2UnmarshalOptions...)
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
	if errors.As(err, &typeErr) {
		return sanitizedJSONTypeError(typeErr.Field, typeErr.Type)
	}
	// v2 引擎返回 *jsonv2.SemanticError，消息里同样带 Go 类型名
	// （"Go uint"、切片或结构体定义），必须走同一套清洗。
	var semanticErr *jsonv2.SemanticError
	if errors.As(err, &semanticErr) && semanticErr.GoType != nil {
		return sanitizedJSONTypeError(jsonPointerToField(string(semanticErr.JSONPointer)), semanticErr.GoType)
	}
	return err
}

// jsonPointerToField 把 RFC 6901 指针（/messages/0/role）转成客户端可读的
// 点分字段路径（messages.0.role），并处理 ~0/~1 转义。
func jsonPointerToField(pointer string) string {
	if pointer == "" {
		return ""
	}
	segments := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for i, segment := range segments {
		segment = strings.ReplaceAll(segment, "~1", "/")
		segment = strings.ReplaceAll(segment, "~0", "~")
		segments[i] = segment
	}
	return strings.Join(segments, ".")
}

func sanitizedJSONTypeError(field string, goType reflect.Type) error {
	if field == "" {
		field = "the request body"
	}
	kind := sanitizeJSONKindName(goType.Kind())
	return fmt.Errorf("json: cannot unmarshal %s into field %s: field %s must be a %s",
		kind, field, field, kind)
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
