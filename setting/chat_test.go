package setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUpdateChatsByJsonStringKeepsExistingDataOnParseError 确认解析失败不会
// 把 Chats 换成空切片。
//
// 旧实现先 make([]map[string]string, 0) 再 Unmarshal，坏 JSON 一提交就把
// 客户端下载配置清空，接口却返回错误。
func TestUpdateChatsByJsonStringKeepsExistingDataOnParseError(t *testing.T) {
	before := Chats2JsonString()

	err := UpdateChatsByJsonString(`[{"a":"b"},`)
	require.Error(t, err)
	require.Equal(t, before, Chats2JsonString(), "a failed parse must not touch Chats")
}

func TestUpdateChatsByJsonStringReplacesOnSuccess(t *testing.T) {
	original := Chats2JsonString()
	t.Cleanup(func() { require.NoError(t, UpdateChatsByJsonString(original)) })

	require.NoError(t, UpdateChatsByJsonString(`[{"Only":"https://example.com"}]`))
	require.JSONEq(t, `[{"Only":"https://example.com"}]`, Chats2JsonString())
}

// TestUpdateUserUsableGroupsByJSONStringKeepsExistingDataOnParseError 同上。
func TestUpdateUserUsableGroupsByJSONStringKeepsExistingDataOnParseError(t *testing.T) {
	before := GetUserUsableGroupsCopy()
	require.NotEmpty(t, before)

	err := UpdateUserUsableGroupsByJSONString(`{"default":`)
	require.Error(t, err)
	require.Equal(t, before, GetUserUsableGroupsCopy(), "a failed parse must not touch the group map")
}
