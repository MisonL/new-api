package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling - 后结算辅助函数
// ---------------------------------------------------------------------------

// settleErrorLogField 是结算失败写进消费日志 other 的键。
const settleErrorLogField = "settle_error"

// MarkSettleFailure 把结算失败写进消费日志的 other 字段，并记录带身份字段的
// 错误日志。结算发生在响应写完之后：HTTP 状态已不可改，而 used_quota 与
// 渠道用量已经累加、消费日志也已写入。失败只留在服务端 stderr 时，这笔差额
// 无法对账，因此必须落进日志行本身。
//
// 这里不重试：IncreaseUserQuota 是 quota += N 的非幂等操作，重试可能重复退款。
func MarkSettleFailure(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int, settleErr error, other map[string]interface{}) {
	if settleErr == nil {
		return
	}
	if other != nil {
		other[settleErrorLogField] = settleErr.Error()
	}
	userId, channelId, tokenId := 0, 0, 0
	if relayInfo != nil {
		userId, channelId, tokenId = relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId
	}
	logger.LogError(ctx, fmt.Sprintf("error settling billing: %s (userId %d, channelId %d, tokenId %d, actualQuota %d)",
		settleErr.Error(), userId, channelId, tokenId, actualQuota))
}

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		return PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true)
	}
	return nil
}
