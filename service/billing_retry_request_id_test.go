package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 订阅预扣以请求 ID 做幂等键，退款会把该键标记为 refunded，此后同一
// requestId 再预扣会被拒绝（见 TestReusedRefundedRequestIdIsRejectedOnPreConsume）。
// responses bootstrap 恢复在退款后沿用同一 requestId 重建计费会话，
// 订阅用户会在第二次尝试时被直接弹出，恢复机制对订阅用户实际不可用。
func TestBillingAttemptKeyRotatesForBootstrapRecovery(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	model.KeyColumn()

	const userID, tokenID, subID = 52, 52, 52
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-bootstrap-key", 100000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id: 5201, Title: "bootstrap-retry-plan", Enabled: true,
		DurationUnit: "month", DurationValue: 1, TotalAmount: 1_000_000,
		QuotaResetPeriod: model.SubscriptionResetNever,
	}).Error)
	seedSubscription(t, subID, userID, 1_000_000, 0)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subID).
		Update("plan_id", 5201).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("id", userID)

	info := newRealtimeProbeInfo(userID, tokenID, "sk-bootstrap-key")
	info.RequestId = "original-request-id"
	info.BillingAttemptKey = "original-request-id" // NewRelayInfo 会做的初始化
	info.ForcePreConsume = true
	info.UserQuota = 1_000_000

	session, apiErr := NewBillingSession(c, info, 100)
	require.Nil(t, apiErr, "首轮预扣必须成功")
	firstKey := info.BillingAttemptKey
	session.Refund(c)
	// Refund 异步执行；单测库是文件级 :memory: 共享连接，轮询等待记录落为 refunded。
	require.Eventually(t, func() bool {
		var n int64
		model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("status = ?", "refunded").Count(&n)
		return n == 1
	}, 5*time.Second, 20*time.Millisecond)

	// 与生产一致：bootstrap 恢复先经 ResetBillingMetadata 退还旧预扣并重建会话。
	// 这里第一轮已显式 Refund，等价于非信任用户的首轮失败退款。
	info.ResetBillingMetadata(c)
	require.NotEqual(t, firstKey, info.BillingAttemptKey, "重建计费会话时必须轮换预扣键")
	session2, apiErr := NewBillingSession(c, info, 100)
	require.Nil(t, apiErr, "恢复重试必须能重新预扣，而不是撞上已退款的幂等键")
	require.NotNil(t, session2)
	require.NotEmpty(t, firstKey)
	require.NotEqual(t, firstKey, info.BillingAttemptKey, "重试必须换用新的预扣键")
	var consumed int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).
		Where("status = ?", "consumed").Count(&consumed).Error)
	require.EqualValues(t, 1, consumed)
	var refunded int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).
		Where("status = ?", "refunded").Count(&refunded).Error)
	require.EqualValues(t, 1, refunded, "首轮退款已经提交，重试不得复用该键")
}

// 已退款的旧键再预扣会被拒绝，这是必须轮换键的根本原因。守卫这条语义，
// 防止有人把 PreConsumeUserSubscription 的已退款分支放宽来实现「恢复」。
func TestReusedRefundedRequestIdIsRejectedOnPreConsume(t *testing.T) {
	truncate(t)
	const userID, subID = 53, 53
	seedSubscription(t, subID, userID, 100000, 0)

	requestId := "bootstrap-retry-reused-key"
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestId,
		UserId:             userID,
		UserSubscriptionId: subID,
		PreConsumed:        100,
		Status:             "refunded",
	}).Error)

	_, err := model.PreConsumeUserSubscription(requestId, userID, "probe-model", 0, 100)
	require.Error(t, err, "已退款的幂等键不得被再次预扣")
}
