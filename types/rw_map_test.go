package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoadFromJsonStringKeepsExistingDataOnParseError 锁定失败语义：解析
// 失败的 JSON 不能清空已有内容。
//
// 旧实现先 m.data = make(map[K]V) 再 Unmarshal。管理端提交一个被截断的倍率
// JSON 时，接口返回错误，但内存里的整张倍率表已经空了；该值还会先落库，于是
// 每 60 秒的选项同步会反复把空表加载回来，直到有人写入合法值。
func TestLoadFromJsonStringKeepsExistingDataOnParseError(t *testing.T) {
	m := NewRWMap[string, float64]()
	require.NoError(t, LoadFromJsonString(m, `{"gpt-4": 15, "claude": 3}`))
	before := m.ReadAll()
	require.Len(t, before, 2)

	err := LoadFromJsonString(m, `{"gpt-4": 20,`)
	require.Error(t, err)

	after := m.ReadAll()
	require.Equal(t, before, after, "a failed parse must not touch the existing map")
}

// TestLoadFromJsonStringWithCallbackKeepsExistingDataOnParseError 同上，
// 并额外确认失败时不会触发回调（回调会让定价快照缓存失效，误触发会把空表
// 推给所有读者）。
func TestLoadFromJsonStringWithCallbackKeepsExistingDataOnParseError(t *testing.T) {
	m := NewRWMap[string, float64]()
	require.NoError(t, LoadFromJsonString(m, `{"a": 1}`))

	callbackRan := false
	err := LoadFromJsonStringWithCallback(m, `not json`, func() { callbackRan = true })
	require.Error(t, err)
	require.False(t, callbackRan, "callback must not run on parse failure")

	value, ok := m.Get("a")
	require.True(t, ok)
	require.Equal(t, 1.0, value)
}

// TestLoadFromJsonStringWithCallbackRunsCallbackOnSuccess 确认成功路径仍然
// 触发回调并把数据换成新内容。
func TestLoadFromJsonStringWithCallbackRunsCallbackOnSuccess(t *testing.T) {
	m := NewRWMap[string, float64]()
	require.NoError(t, LoadFromJsonString(m, `{"a": 1}`))

	callbackRan := false
	require.NoError(t, LoadFromJsonStringWithCallback(m, `{"b": 2}`, func() { callbackRan = true }))
	require.True(t, callbackRan)

	_, hasA := m.Get("a")
	require.False(t, hasA, "successful load replaces the map")
	value, ok := m.Get("b")
	require.True(t, ok)
	require.Equal(t, 2.0, value)
}

// TestLoadFromJsonStringRejectsNullPayload 确认 "null" 不会把 map 换成 nil
// 数据源（读操作会拿到 nil map，语义等同清空）。
func TestLoadFromJsonStringRejectsNullPayload(t *testing.T) {
	m := NewRWMap[string, float64]()
	require.NoError(t, LoadFromJsonString(m, `{"a": 1}`))

	// Unmarshal("null") 成功但会把 data 置为 nil；这里要求保持原状。
	err := LoadFromJsonString(m, `null`)
	if err == nil {
		value, ok := m.Get("a")
		require.True(t, ok, "a nil payload must not erase the map")
		require.Equal(t, 1.0, value)
	}
}
