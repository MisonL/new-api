package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalizedClaudeToolParametersPreservesPropertyOrder 验证 normalizeClaudeInputSchema
// 序列化后的 JSON Schema 保持客户端定义的属性顺序，而非 map 的字典序。
func TestNormalizedClaudeToolParametersPreservesPropertyOrder(t *testing.T) {
	// Given: 客户端按 reasoning → answer → thought 定义的 schema
	rawSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"reasoning": {"type": "string"},
			"answer": {"type": "string"},
			"thought": {"type": "string"}
		},
		"required": ["reasoning", "answer"]
	}`)

	// When: 经过规范化处理（入参就是 RawMessage，保持原始字节）
	result, err := normalizeClaudeInputSchema(rawSchema, "test-tool")
	require.NoError(t, err, "normalizeClaudeInputSchema")

	marshalled, err := json.Marshal(result)
	require.NoError(t, err, "Marshal normalized schema")

	// Then: 属性顺序仍保持 reasoning → answer → thought，而不是字典序 answer → reasoning → thought
	str := string(marshalled)
	reasoningIdx := strings.Index(str, `"reasoning"`)
	answerIdx := strings.Index(str, `"answer"`)
	thoughtIdx := strings.Index(str, `"thought"`)

	require.NotEqual(t, -1, reasoningIdx, "reasoning key must be present")
	require.NotEqual(t, -1, answerIdx, "answer key must be present")
	require.NotEqual(t, -1, thoughtIdx, "thought key must be present")
	require.Less(t, reasoningIdx, answerIdx, "reasoning must appear before answer in marshalled JSON")
	require.Less(t, answerIdx, thoughtIdx, "answer must appear before thought in marshalled JSON")
}
