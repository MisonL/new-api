package common

import (
	jsonv2 "encoding/json/v2"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 解码引擎切到 encoding/json/v2 后，类型错误来自 *jsonv2.SemanticError。
// 这条测试是引擎切换的探针：若有人把 Unmarshal 改回 v1 实现，它会立刻失败。
func TestUnmarshalUsesJSONV2Engine(t *testing.T) {
	var target probeRequest
	err := Unmarshal([]byte(`{"max_tokens":"abc"}`), &target)
	require.Error(t, err)

	var semanticErr *jsonv2.SemanticError
	require.ErrorAs(t, err, &semanticErr)
}

// v2 引擎必须保留 v1 的三条宽容语义，否则会改变网关接受请求的边界：
// 重复键后者生效、字段名大小写不敏感、非法 UTF-8 用替换字符而不是报错。
func TestUnmarshalKeepsV1Compatibility(t *testing.T) {
	t.Run("duplicate member names last wins", func(t *testing.T) {
		var target struct {
			Name string `json:"name"`
		}
		require.NoError(t, Unmarshal([]byte(`{"name":"first","name":"second"}`), &target))
		require.Equal(t, "second", target.Name)
	})

	t.Run("case insensitive field names", func(t *testing.T) {
		var target struct {
			Name string `json:"name"`
		}
		require.NoError(t, Unmarshal([]byte(`{"NAME":"cap"}`), &target))
		require.Equal(t, "cap", target.Name)
	})

	t.Run("invalid utf8 replaced", func(t *testing.T) {
		var target struct {
			Name string `json:"name"`
		}
		require.NoError(t, Unmarshal([]byte("{\"name\":\"x\xffy\"}"), &target))
		require.Equal(t, "x�y", target.Name)
	})

	t.Run("trailing garbage rejected", func(t *testing.T) {
		var target struct {
			Name string `json:"name"`
		}
		require.Error(t, Unmarshal([]byte(`{"name":"a"} {"name":"b"}`), &target))
	})
}

// RawMessage 与 json.Number 是绕过字段匹配的类型，切换引擎后必须保持同样的值。
func TestUnmarshalKeepsRawMessageAndNumber(t *testing.T) {
	var target struct {
		Meta RawMessage `json:"meta"`
		Num  JsonNumber `json:"num"`
	}
	require.NoError(t, Unmarshal([]byte(`{"meta":{"a":1},"num":123456789012345678}`), &target))
	require.JSONEq(t, `{"a":1}`, string(target.Meta))
	require.Equal(t, "123456789012345678", target.Num.String())
}

// DecodeJson 是流式读取，必须保持 v1 Decoder.Decode 的语义：只消费第一个
// JSON 值，顶层值之后的内容既不消费也不报错；空输入返回 io.EOF。
func TestDecodeJsonKeepsV1StreamingSemantics(t *testing.T) {
	var target struct {
		Name string `json:"name"`
	}
	require.NoError(t, DecodeJson(strings.NewReader(`{"name":"first"}{"name":"second"}`), &target))
	require.Equal(t, "first", target.Name)

	err := DecodeJson(strings.NewReader(""), &target)
	require.ErrorIs(t, err, io.EOF)
}

// v2 的类型错误带有 Go 类型名（"Go uint"、切片/结构体定义），
// SanitizeJSONDecodeError 必须同样把它们清洗掉并保留字段名。
func TestSanitizeJSONDecodeErrorHandlesJSONV2SemanticError(t *testing.T) {
	var target probeRequest
	err := jsonv2.Unmarshal([]byte(`{"max_tokens":"abc"}`), &target)
	var semanticErr *jsonv2.SemanticError
	require.ErrorAs(t, err, &semanticErr)

	sanitized := SanitizeJSONDecodeError(err)
	require.Contains(t, sanitized.Error(), "max_tokens")
	require.Contains(t, sanitized.Error(), "must be a")
	require.NotContains(t, sanitized.Error(), "Go ")

	var nested probeRequestWithMessages
	err = jsonv2.Unmarshal([]byte(`{"messages":[{"role":1}]}`), &nested)
	require.ErrorAs(t, err, &semanticErr)
	sanitized = SanitizeJSONDecodeError(err)
	require.Contains(t, sanitized.Error(), "messages.0.role")
	require.NotContains(t, sanitized.Error(), "probeMessage")
	require.NotContains(t, sanitized.Error(), "Go ")
}
