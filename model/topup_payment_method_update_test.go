package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Epay 回调发现"实际支付方式与订单不同"时，只应更新 payment_method 这一列。
// 用 TopUp.Update()（DB.Save 全字段）会把调用方手上的旧快照整体写回：
// 并发的结算若已把订单写成 success，会被退回 pending，回调重放随后再次
// 执行 quota + ?，形成重复入账。
func TestUpdatePendingTopUpPaymentMethodDoesNotResetSettledOrder(t *testing.T) {
	db := setupTopUpPaymentSecurityTestDB(t)

	topUp := &TopUp{
		TradeNo:         "USR1NO-update-payment-method",
		UserId:          1,
		Amount:          100,
		Money:           10,
		Status:          common.TopUpStatusSuccess,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, db.Create(topUp).Error)

	// 订单已结算：条件更新必须拒绝，且不能改动任何字段。
	err := UpdatePendingTopUpPaymentMethod(topUp.TradeNo, PaymentProviderEpay, "wxpay")
	require.NoError(t, err, "a no-op against a settled order is not an error")

	stored := &TopUp{}
	require.NoError(t, db.Where("trade_no = ?", topUp.TradeNo).First(stored).Error)
	require.Equal(t, common.TopUpStatusSuccess, stored.Status, "a settled order must stay settled")
	require.Equal(t, "alipay", stored.PaymentMethod, "a settled order's payment method must not change")

	// pending 订单则允许更新支付方式，并只改这一列。
	pending := &TopUp{
		TradeNo:         "USR1NO-update-payment-method-pending",
		UserId:          1,
		Amount:          100,
		Money:           10,
		Status:          common.TopUpStatusPending,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, db.Create(pending).Error)
	require.NoError(t, UpdatePendingTopUpPaymentMethod(pending.TradeNo, PaymentProviderEpay, "wxpay"))

	storedPending := &TopUp{}
	require.NoError(t, db.Where("trade_no = ?", pending.TradeNo).First(storedPending).Error)
	require.Equal(t, "wxpay", storedPending.PaymentMethod)
	require.Equal(t, common.TopUpStatusPending, storedPending.Status)

	// 网关不匹配时必须不动别人的订单。
	other := &TopUp{
		TradeNo:         "USR1NO-update-payment-method-other",
		UserId:          1,
		Amount:          100,
		Money:           10,
		Status:          common.TopUpStatusPending,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderStripe,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, db.Create(other).Error)
	require.NoError(t, UpdatePendingTopUpPaymentMethod(other.TradeNo, PaymentProviderEpay, "wxpay"))

	storedOther := &TopUp{}
	require.NoError(t, db.Where("trade_no = ?", other.TradeNo).First(storedOther).Error)
	require.Equal(t, "alipay", storedOther.PaymentMethod, "a different gateway's order must not be touched")
}
