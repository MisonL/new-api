package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// UpdateAbilityByTag 用结构体 Updates，GORM 跳过零值字段：管理员把标签
// 权重清零时 Weight=0 不写库，接口返回成功但路由仍用旧权重。
func TestUpdateAbilityByTagCanClearWeightToZero(t *testing.T) {
	db := openTestDB(t, &Ability{}, &Channel{})
	previousDB := DB
	DB = db
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCache
	})

	tag := "zero-weight-tag"
	require.NoError(t, db.Create(&Ability{
		Group: "default", Model: "m1", ChannelId: 9501, Enabled: true,
		Weight: 50, Tag: &tag,
	}).Error)

	zero := uint(0)
	require.NoError(t, UpdateAbilityByTag(tag, nil, nil, &zero))

	var reloaded Ability
	require.NoError(t, db.Where("channel_id = ? AND model = ?", 9501, "m1").First(&reloaded).Error)
	require.Zero(t, reloaded.Weight, "显式清零必须写库")
}
