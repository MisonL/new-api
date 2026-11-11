package service

import (
	"io"
	"log"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 用量统计是 SQL 相对增量，写入失败后没有可回放的来源。失败必须落在消费
// 日志行本身，否则既不知道是哪个请求丢的增量，也无法对账。
func TestPostWssConsumeQuotaRecordsUsageStatsFailureInConsumeLog(t *testing.T) {
	setupRealtimeBillingProbe(t)

	seedUser(t, 6, 1_000_000)
	seedToken(t, 6, 6, "usage-stats-failure-probe-key", 1_000_000)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(6, 6, "usage-stats-failure-probe-key")
	// 关闭结算差额：本用例只观察用量统计写入失败。
	info.FinalPreConsumedQuota = realtimeTurnQuota

	// 统计写入走 model.DB；消费日志走 model.LOG_DB。把 DB 换到一张没有
	// users/channels 表的库，统计必然失败，日志仍能写入真实库。
	broken, err := gorm.Open(sqlite.Open("file:usage_stats_broken?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.New(log.New(io.Discard, "", 0), gormlogger.Config{
			LogLevel:                  gormlogger.Silent,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
	})
	require.NoError(t, err)
	previousDB := model.DB
	model.DB = broken
	t.Cleanup(func() { model.DB = previousDB })

	PostWssConsumeQuota(c, info, realtimeDoubleChargeModel, realtimeTextTurn(realtimeTurnQuota), "")

	var logRow model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 6, model.LogTypeConsume).
		Order("id desc").First(&logRow).Error)

	other, err := common.StrToMap(logRow.Other)
	require.NoError(t, err)
	require.Contains(t, other, "usage_stats_error", "用量统计写入失败必须能在消费日志里筛出来")
	require.Contains(t, other["usage_stats_error"], "users")
}
