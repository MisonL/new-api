package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSubscriptionPaymentGuardTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousDB := DB
	previousLogDB := LOG_DB

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	DB = db
	LOG_DB = db

	require.NoError(t, db.AutoMigrate(&User{}, &SubscriptionPlan{}, &SubscriptionOrder{}, &UserSubscription{}, &TopUp{}, &Log{}))

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		DB = previousDB
		LOG_DB = previousLogDB

		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func seedSubscriptionOrderGuardFixture(t *testing.T, db *gorm.DB, paymentMethod string, paymentProvider string, tradeNo string) *SubscriptionOrder {
	t.Helper()

	user := &User{
		Id:       1,
		Username: "subscription-guard-user",
		Password: "Password123!",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(user).Error)

	plan := &SubscriptionPlan{
		Id:               1,
		Title:            "Guard Plan",
		PriceAmount:      9.9,
		Currency:         "USD",
		DurationUnit:     SubscriptionDurationDay,
		DurationValue:    30,
		TotalAmount:      1000,
		Enabled:          true,
		UpgradeGroup:     "",
		CreatedAt:        common.GetTimestamp(),
		UpdatedAt:        common.GetTimestamp(),
		QuotaResetPeriod: SubscriptionResetNever,
	}
	require.NoError(t, db.Create(plan).Error)

	order := &SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentMethod,
		PaymentProvider: paymentProvider,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, db.Create(order).Error)
	return order
}

func TestCompleteSubscriptionOrderWithPaymentMethodRejectsMismatch(t *testing.T) {
	db := setupSubscriptionPaymentGuardTestDB(t)
	order := seedSubscriptionOrderGuardFixture(t, db, PaymentProviderCreem, PaymentProviderCreem, "sub-creem-order")

	err := CompleteSubscriptionOrderWithPaymentMethod(order.TradeNo, "{}", PaymentProviderStripe)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	var refreshed SubscriptionOrder
	require.NoError(t, db.Where("trade_no = ?", order.TradeNo).First(&refreshed).Error)
	require.Equal(t, common.TopUpStatusPending, refreshed.Status)
}

func TestCompleteSubscriptionOrderWithPaymentMethodAcceptsEpayFamily(t *testing.T) {
	db := setupSubscriptionPaymentGuardTestDB(t)
	order := seedSubscriptionOrderGuardFixture(t, db, "alipay", PaymentProviderEpay, "sub-epay-order")

	err := CompleteSubscriptionOrderWithPaymentMethod(order.TradeNo, `{"provider":"epay"}`, PaymentProviderEpay)
	require.NoError(t, err)

	var refreshed SubscriptionOrder
	require.NoError(t, db.Where("trade_no = ?", order.TradeNo).First(&refreshed).Error)
	require.Equal(t, common.TopUpStatusSuccess, refreshed.Status)

	var count int64
	require.NoError(t, db.Model(&UserSubscription{}).Where("user_id = ?", order.UserId).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestExpireSubscriptionOrderWithPaymentMethodRejectsMismatch(t *testing.T) {
	db := setupSubscriptionPaymentGuardTestDB(t)
	order := seedSubscriptionOrderGuardFixture(t, db, PaymentProviderStripe, PaymentProviderStripe, "sub-stripe-order")

	err := ExpireSubscriptionOrderWithPaymentMethod(order.TradeNo, PaymentProviderCreem)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	var refreshed SubscriptionOrder
	require.NoError(t, db.Where("trade_no = ?", order.TradeNo).First(&refreshed).Error)
	require.Equal(t, common.TopUpStatusPending, refreshed.Status)
}
