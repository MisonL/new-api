package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestExpirePendingTopUpByTradeNoForPaymentMethodRejectsMismatch(t *testing.T) {
	db := setupTopUpPaymentSecurityTestDB(t)

	user := &User{
		Id:       1,
		Username: "topup-expire-user",
		Password: "Password123!",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	topUp := &TopUp{
		UserId:        user.Id,
		Amount:        100,
		Money:         8,
		TradeNo:       "topup-stripe-order",
		PaymentMethod: PaymentProviderStripe,
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())

	err := ExpirePendingTopUpByTradeNoForPaymentMethod(topUp.TradeNo, PaymentProviderWaffo)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	var refreshed TopUp
	require.NoError(t, db.Where("trade_no = ?", topUp.TradeNo).First(&refreshed).Error)
	require.Equal(t, common.TopUpStatusPending, refreshed.Status)
}

func TestMarkPendingTopUpFailedByTradeNoForPaymentMethodRejectsMismatch(t *testing.T) {
	db := setupTopUpPaymentSecurityTestDB(t)

	user := &User{
		Id:       1,
		Username: "topup-failed-user",
		Password: "Password123!",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	topUp := &TopUp{
		UserId:        user.Id,
		Amount:        100,
		Money:         8,
		TradeNo:       "topup-creem-order",
		PaymentMethod: PaymentProviderCreem,
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())

	err := MarkPendingTopUpFailedByTradeNoForPaymentMethod(topUp.TradeNo, PaymentProviderWaffo)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	var refreshed TopUp
	require.NoError(t, db.Where("trade_no = ?", topUp.TradeNo).First(&refreshed).Error)
	require.Equal(t, common.TopUpStatusPending, refreshed.Status)
}
