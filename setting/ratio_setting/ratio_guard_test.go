package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 负倍率不是"便宜"，而是反向计费：text_quota 为负时 PostConsumeQuota 走
// IncreaseUserQuota，等于给用户加钱。GroupRatio 早有 CheckGroupRatio 拒绝
// 负值，其余倍率表没有任何守卫，任何一条写路径都能把负值写进去。
func TestRatioSettersRejectNegativeValues(t *testing.T) {
	cases := []struct {
		name string
		set  func(string) error
		get  func() map[string]float64
	}{
		{"ModelRatio", UpdateModelRatioByJSONString, modelRatioMap.ReadAll},
		{"ModelPrice", UpdateModelPriceByJSONString, modelPriceMap.ReadAll},
		{"CompletionRatio", UpdateCompletionRatioByJSONString, completionRatioMap.ReadAll},
		{"CacheRatio", UpdateCacheRatioByJSONString, cacheRatioMap.ReadAll},
		{"CreateCacheRatio", UpdateCreateCacheRatioByJSONString, createCacheRatioMap.ReadAll},
		{"ImageRatio", UpdateImageRatioByJSONString, imageRatioMap.ReadAll},
		{"AudioRatio", UpdateAudioRatioByJSONString, audioRatioMap.ReadAll},
		{"AudioCompletionRatio", UpdateAudioCompletionRatioByJSONString, audioCompletionRatioMap.ReadAll},
		{"GroupRatio", UpdateGroupRatioByJSONString, groupRatioMap.ReadAll},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.get()

			err := tc.set(`{"negative-guard-model":-0.5}`)
			require.Error(t, err, "negative ratio must be rejected")
			require.Contains(t, err.Error(), "negative-guard-model",
				"the error must name the offending entry")
			require.Equal(t, before, tc.get(), "a rejected payload must not touch the table")

			// 0 是合法值：未定价模型靠它走 fail-closed 分支。
			require.NoError(t, tc.set(`{"negative-guard-model":0}`))
			value, ok := tc.get()["negative-guard-model"]
			require.True(t, ok)
			require.Zero(t, value)
		})
	}
}

func TestUpdateGroupGroupRatioRejectsNegativeValues(t *testing.T) {
	before := groupGroupRatioMap.ReadAll()

	err := UpdateGroupGroupRatioByJSONString(`{"vip":{"negative-guard-group":-1}}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "negative-guard-group")
	require.Equal(t, before, groupGroupRatioMap.ReadAll())
}
