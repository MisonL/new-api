package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// failingFunding 模拟资金来源结算失败（例如落账时数据库不可用）。
type failingFunding struct{}

func (failingFunding) Source() string       { return BillingSourceWallet }
func (failingFunding) PreConsume(int) error { return nil }
func (failingFunding) Settle(int) error     { return errors.New("injected settle failure") }
func (failingFunding) Refund() error        { return nil }

// 结算发生在响应写完之后，HTTP 状态已不可改。此时 used_quota 与渠道用量已经
// 累加、消费日志也已经写入，SettleBilling 的差额失败若只留在服务端 stderr，
// 这笔缺口就无法对账。失败必须落在消费日志行本身，能被筛出来。
func TestPostWssConsumeQuotaRecordsSettleFailureInConsumeLog(t *testing.T) {
	setupRealtimeBillingProbe(t)

	seedUser(t, 5, 1_000_000)
	seedToken(t, 5, 5, "settle-failure-probe-key", 1_000_000)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(5, 5, "settle-failure-probe-key")
	info.Billing = &BillingSession{
		relayInfo:        info,
		funding:          failingFunding{},
		preConsumedQuota: 0,
	}

	PostWssConsumeQuota(c, info, realtimeDoubleChargeModel, realtimeTextTurn(realtimeTurnQuota), "")

	var log model.Log
	require.NoError(t, model.DB.Where("user_id = ? AND type = ?", 5, model.LogTypeConsume).
		Order("id desc").First(&log).Error)

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Contains(t, other, "settle_error", "结算失败必须能在消费日志里筛出来")
	require.Contains(t, other["settle_error"], "injected settle failure")
}
