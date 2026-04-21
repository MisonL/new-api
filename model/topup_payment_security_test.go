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

func setupTopUpPaymentSecurityTestDB(t *testing.T) *gorm.DB {
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

	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}))

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

func TestRechargeRejectsNonStripeTopUpOrders(t *testing.T) {
	db := setupTopUpPaymentSecurityTestDB(t)

	user := &User{
		Id:       1,
		Username: "stripe-security-user",
		Password: "Password123!",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	topUp := &TopUp{
		UserId:        user.Id,
		Amount:        100,
		Money:         12.5,
		TradeNo:       "test-epay-order",
		PaymentMethod: "epay",
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())

	err := Recharge(topUp.TradeNo, "cus_test", "127.0.0.1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "充值失败")

	var refreshedTopUp TopUp
	require.NoError(t, db.Where("trade_no = ?", topUp.TradeNo).First(&refreshedTopUp).Error)
	require.Equal(t, common.TopUpStatusPending, refreshedTopUp.Status)

	var refreshedUser User
	require.NoError(t, db.First(&refreshedUser, user.Id).Error)
	require.Zero(t, refreshedUser.Quota)
	require.Empty(t, refreshedUser.StripeCustomer)
}
