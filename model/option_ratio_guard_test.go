package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

// 负倍率必须在落库前被拒。若只在 updateOptionMap 的派发阶段拦，DB 里已经写
// 进了坏值，只是内存表没更新：管理员界面显示负倍率、下次重启回载时又被拒，
// 状态长期不一致。
func TestUpdateOptionRejectsNegativeModelRatioBeforePersist(t *testing.T) {
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
		common.OptionMap = previousOptionMap
	})

	before := ratio_setting.GetModelRatioCopy()

	err := UpdateOption("ModelRatio", `{"negative-guard-model":-0.5}`)
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "ModelRatio").Count(&count).Error)
	require.Zero(t, count, "a rejected ratio must not reach the database")
	require.NotContains(t, common.OptionMap, "ModelRatio")
	require.Equal(t, before, ratio_setting.GetModelRatioCopy())

	// 非负值照常接受，0 也在内（未定价模型的 fail-closed 依赖 0）。
	require.NoError(t, UpdateOption("ModelRatio", `{"negative-guard-model":0}`))
	require.Contains(t, common.OptionMap, "ModelRatio")
}

func TestUpdateOptionRejectsNegativeModelPriceBeforePersist(t *testing.T) {
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
		common.OptionMap = previousOptionMap
	})

	require.Error(t, UpdateOption("ModelPrice", `{"negative-guard-model":-1}`))

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "ModelPrice").Count(&count).Error)
	require.Zero(t, count)
}

// 分层的 group_ratio_setting.group_ratio 不经过 UpdateGroupRatioByJSONString，
// 而是由通用派发交给分层配置系统，直接反序列化进 RWMap 字段。同一个负值从
// 顶层键被拒、从点号键却被接受。
func TestUpdateOptionRejectsNegativeGroupRatioViaLayeredKey(t *testing.T) {
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
		common.OptionMap = previousOptionMap
	})

	before := ratio_setting.GetGroupRatioCopy()

	err := UpdateOption("group_ratio_setting.group_ratio", `{"vip":-9}`)
	require.Error(t, err)
	require.Equal(t, before, ratio_setting.GetGroupRatioCopy(), "the live table must be untouched")

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "group_ratio_setting.group_ratio").Count(&count).Error)
	require.Zero(t, count)

	// 合法值仍然可以走分层路径写入。
	require.NoError(t, UpdateOption("group_ratio_setting.group_ratio", `{"vip":1.5}`))
	require.Equal(t, 1.5, ratio_setting.GetGroupRatioCopy()["vip"])
}
