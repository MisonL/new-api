package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 任务提交后结算失败时，HTTP 状态早已不可改、任务行也已落库，失败此前只写
// 一行 stderr：这笔差额无法与具体请求对上账。文本/音频/realtime 路径都会把
// 结算失败写进消费日志 other（MarkSettleFailure），任务路径必须一致。

func taskConsumeLogContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("token_name", "test_token")
	c.Set("username", "test_user")
	return c
}

func taskConsumeLogInfo(userId int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          userId,
		TokenId:         5,
		OriginModelName: "sora-2",
		UsingGroup:      "default",
		PriceData: types.PriceData{
			Quota: 100,
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 3,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action: "video.generate",
		},
	}
}

func readTaskConsumeLogOther(t *testing.T, userId int) map[string]interface{} {
	t.Helper()
	var log model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", userId, model.LogTypeConsume).Order("id DESC").First(&log).Error)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	return other
}

func TestLogTaskConsumptionRecordsSettleFailure(t *testing.T) {
	truncate(t)
	seedUser(t, 9701, 1000)

	c := taskConsumeLogContext(t)
	LogTaskConsumption(c, taskConsumeLogInfo(9701), errors.New("wallet refund failed"))

	other := readTaskConsumeLogOther(t, 9701)
	require.Contains(t, other, "settle_error",
		"结算失败必须写进消费日志 other，否则这笔差额无法对账")
	require.Contains(t, other["settle_error"], "wallet refund failed")
	require.Equal(t, true, other["is_task"], "任务日志原有字段不得丢失")
}

func TestLogTaskConsumptionOmitsSettleErrorOnSuccess(t *testing.T) {
	truncate(t)
	seedUser(t, 9702, 1000)

	c := taskConsumeLogContext(t)
	LogTaskConsumption(c, taskConsumeLogInfo(9702), nil)

	other := readTaskConsumeLogOther(t, 9702)
	require.NotContains(t, other, "settle_error", "结算成功时不得留下失败标记")
}
