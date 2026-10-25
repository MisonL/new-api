package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUpdateTopupGroupRatioKeepsExistingDataOnParseError 锁定失败语义：坏
// JSON 只能返回错误，不能把充值分组倍率表换成空表。
func TestUpdateTopupGroupRatioKeepsExistingDataOnParseError(t *testing.T) {
	before := TopupGroupRatio2JSONString()
	require.NotEqual(t, "{}", before)

	err := UpdateTopupGroupRatioByJSONString(`{"default":`)
	require.Error(t, err)
	require.Equal(t, before, TopupGroupRatio2JSONString(), "failed parse must not clear the map")
}

// TestUpdateTopupGroupRatioRejectsNullPayload 确认 "null" 被拒绝，而不是把
// 表换成 nil（后续 Set 会 panic）。
func TestUpdateTopupGroupRatioRejectsNullPayload(t *testing.T) {
	before := TopupGroupRatio2JSONString()
	err := UpdateTopupGroupRatioByJSONString(`null`)
	require.Error(t, err)
	require.Equal(t, before, TopupGroupRatio2JSONString())
}

func TestUpdateTopupGroupRatioReplacesOnSuccess(t *testing.T) {
	original := TopupGroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, UpdateTopupGroupRatioByJSONString(original)) })

	require.NoError(t, UpdateTopupGroupRatioByJSONString(`{"default":2}`))
	require.Equal(t, 2.0, GetTopupGroupRatio("default"))
}
