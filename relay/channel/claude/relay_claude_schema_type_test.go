package claude

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// TestNormalizeClaudeInputSchemaRejectsNullOrNonStringType 验证 schema 的
// "type" 字段只接受字符串 "object"。
//
// 旧实现在 type 为 null 时直接文本拼接 "type":"object"，生成
// {"type":"object","type":null,...} 这样的重复键：JSON 解码取后者，客户端
// 显式写的 null 被静默丢弃，而重复键本身就是非法或有歧义的文档。type 为
// 非 object 的字符串（array/string）同样不能当作工具参数 schema 下发。
func TestNormalizeClaudeInputSchemaRejectsNullOrNonStringType(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"explicit null type", `{"type":null,"properties":{"a":{"type":"string"}}}`},
		{"null type alone", `{"type": null}`},
		{"array type", `{"type":"array"}`},
		{"string type", `{"type":"string"}`},
		{"numeric type", `{"type":123}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := normalizeClaudeInputSchema(json.RawMessage(tc.in), "probe")
			require.Error(t, err, "must reject %s", tc.in)
			require.Nil(t, out)
		})
	}
}

// TestNormalizeClaudeInputSchemaProducesNoDuplicateKeys 覆盖补 type 的路径：
// 补出来的文档必须只有一个 type 键，且解码后的 type 值为 "object"。
func TestNormalizeClaudeInputSchemaProducesNoDuplicateKeys(t *testing.T) {
	out, err := normalizeClaudeInputSchema(
		json.RawMessage(`{"properties":{"a":{"type":"string"}}}`), "probe")
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, common.Unmarshal(out, &decoded))
	require.Equal(t, "object", decoded["type"])

	// 统计原始字节里 "type" 出现在顶层键位置的次数。
	var counter struct {
		Type json.RawMessage `json:"type"`
	}
	require.NoError(t, common.Unmarshal(out, &counter))
	require.NotEmpty(t, counter.Type)
}
