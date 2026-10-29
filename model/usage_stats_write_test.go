package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 用量统计是 SQL 相对增量，写入失败后没有可回放的来源。失败必须返回给
// 调用方，才能写进消费日志、定位到具体请求；只打一行 SysLog 时既不知道
// 是哪个用户/渠道丢了增量，也无法对账。
func TestUpdateUserUsedQuotaAndRequestCountReportsFailure(t *testing.T) {
	db := openTestDB(t) // 不建表：users 表缺失时更新必然失败
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	require.Error(t, UpdateUserUsedQuotaAndRequestCount(1, 100))
}

func TestUpdateChannelUsedQuotaReportsFailure(t *testing.T) {
	db := openTestDB(t) // 不建表：channels 表缺失时更新必然失败
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	require.Error(t, UpdateChannelUsedQuota(1, 100))
}

func TestUpdateUserUsedQuotaAndRequestCountSucceedsWithTable(t *testing.T) {
	db := openTestDB(t, &User{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	require.NoError(t, db.Create(&User{Id: 1, Username: "stats-probe", Quota: 10}).Error)
	require.NoError(t, UpdateUserUsedQuotaAndRequestCount(1, 100))

	var user User
	require.NoError(t, db.Where("id = ?", 1).First(&user).Error)
	require.Equal(t, 100, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)
}
