package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGiftCodeTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	// This helper swaps package globals; do not call it from t.Parallel tests.
	db := openTestDB(t, &User{}, &GiftCode{}, &Log{})
	previousDB := DB
	previousLogDB := LOG_DB
	previousRedisEnabled := common.RedisEnabled
	DB = db
	LOG_DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.RedisEnabled = previousRedisEnabled
	})
	return db
}

func TestGiftCodeAutoMigrate(t *testing.T) {
	db := openTestDB(t, &GiftCode{})
	require.True(t, db.Migrator().HasTable(&GiftCode{}))
	require.True(t, db.Migrator().HasIndex(&GiftCode{}, "idx_gift_codes_code"))
}

func TestCreateAndReceiveGiftCodeMovesQuota(t *testing.T) {
	db := setupGiftCodeTestDB(t)
	creator := User{Id: 1, Username: "creator", Password: "Password123!", AffCode: "creator-aff", Quota: 1000, Status: common.UserStatusEnabled}
	receiver := User{Id: 2, Username: "receiver", Password: "Password123!", AffCode: "receiver-aff", Email: "receiver@example.com", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&creator).Error)
	require.NoError(t, db.Create(&receiver).Error)

	bindType, bindValue, boundUser, err := NormalizeGiftCodeReceiver(GiftCodeReceiverTypeEmail, receiver.Email)
	require.NoError(t, err)
	require.Equal(t, receiver.Id, boundUser.Id)

	giftCode, err := CreateGiftCode(creator.Id, 300, boundUser.Id, bindType, bindValue, "hello", common.GetTimestamp()+3600)
	require.NoError(t, err)
	require.NotEmpty(t, giftCode.Code)

	var refreshedCreator User
	require.NoError(t, db.First(&refreshedCreator, creator.Id).Error)
	require.Equal(t, 700, refreshedCreator.Quota)

	received, err := ReceiveGiftCode(giftCode.Code, receiver.Id, "thanks")
	require.NoError(t, err)
	require.Equal(t, GiftCodeStatusUsed, received.Status)
	require.Equal(t, receiver.Id, received.ReceivedUserId)
	require.Equal(t, "thanks", received.ThankMessage)

	var refreshedReceiver User
	require.NoError(t, db.First(&refreshedReceiver, receiver.Id).Error)
	require.Equal(t, 400, refreshedReceiver.Quota)
}

func TestReceiveGiftCodeRejectsWrongBoundUser(t *testing.T) {
	db := setupGiftCodeTestDB(t)
	creator := User{Id: 1, Username: "creator", Password: "Password123!", AffCode: "creator-aff", Quota: 1000, Status: common.UserStatusEnabled}
	receiver := User{Id: 2, Username: "receiver", Password: "Password123!", AffCode: "receiver-aff", Quota: 100, Status: common.UserStatusEnabled}
	other := User{Id: 3, Username: "other", Password: "Password123!", AffCode: "other-aff", Quota: 50, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&creator).Error)
	require.NoError(t, db.Create(&receiver).Error)
	require.NoError(t, db.Create(&other).Error)

	giftCode, err := CreateGiftCode(creator.Id, 300, receiver.Id, GiftCodeReceiverTypeUserId, "2", "", 0)
	require.NoError(t, err)

	_, err = ReceiveGiftCode(giftCode.Code, other.Id, "wrong user")
	require.ErrorContains(t, err, "绑定其他用户")

	var refreshedOther User
	require.NoError(t, db.First(&refreshedOther, other.Id).Error)
	require.Equal(t, 50, refreshedOther.Quota)
}

func TestCreateGiftCodeRejectsInsufficientQuota(t *testing.T) {
	db := setupGiftCodeTestDB(t)
	creator := User{Id: 1, Username: "creator", Password: "Password123!", AffCode: "creator-aff", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&creator).Error)

	_, err := CreateGiftCode(creator.Id, 300, 0, "", "", "", 0)
	require.ErrorContains(t, err, "余额不足")

	var refreshedCreator User
	require.NoError(t, db.First(&refreshedCreator, creator.Id).Error)
	require.Equal(t, 100, refreshedCreator.Quota)
}

func TestReceiveGiftCodeRejectsExpiredCode(t *testing.T) {
	db := setupGiftCodeTestDB(t)
	creator := User{Id: 1, Username: "creator", Password: "Password123!", AffCode: "creator-aff", Quota: 1000, Status: common.UserStatusEnabled}
	receiver := User{Id: 2, Username: "receiver", Password: "Password123!", AffCode: "receiver-aff", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&creator).Error)
	require.NoError(t, db.Create(&receiver).Error)

	giftCode, err := CreateGiftCode(creator.Id, 300, 0, "", "", "", common.GetTimestamp()-1)
	require.NoError(t, err)

	_, err = ReceiveGiftCode(giftCode.Code, receiver.Id, "")
	require.ErrorContains(t, err, "已过期")

	var refreshedReceiver User
	require.NoError(t, db.First(&refreshedReceiver, receiver.Id).Error)
	require.Equal(t, 100, refreshedReceiver.Quota)
}

func TestReceiveGiftCodeRejectsDuplicateReceive(t *testing.T) {
	db := setupGiftCodeTestDB(t)
	creator := User{Id: 1, Username: "creator", Password: "Password123!", AffCode: "creator-aff", Quota: 1000, Status: common.UserStatusEnabled}
	receiver := User{Id: 2, Username: "receiver", Password: "Password123!", AffCode: "receiver-aff", Quota: 100, Status: common.UserStatusEnabled}
	other := User{Id: 3, Username: "other", Password: "Password123!", AffCode: "other-aff", Quota: 50, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&creator).Error)
	require.NoError(t, db.Create(&receiver).Error)
	require.NoError(t, db.Create(&other).Error)

	giftCode, err := CreateGiftCode(creator.Id, 300, 0, "", "", "", 0)
	require.NoError(t, err)
	_, err = ReceiveGiftCode(giftCode.Code, receiver.Id, "thanks")
	require.NoError(t, err)

	_, err = ReceiveGiftCode(giftCode.Code, other.Id, "again")
	require.ErrorContains(t, err, "不可接收")

	var refreshedOther User
	require.NoError(t, db.First(&refreshedOther, other.Id).Error)
	require.Equal(t, 50, refreshedOther.Quota)
}

func TestReceiveGiftCodeRejectsMissingUserWithoutConsumingCode(t *testing.T) {
	db := setupGiftCodeTestDB(t)
	creator := User{Id: 1, Username: "creator", Password: "Password123!", AffCode: "creator-aff", Quota: 1000, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&creator).Error)

	giftCode, err := CreateGiftCode(creator.Id, 300, 0, "", "", "", 0)
	require.NoError(t, err)

	_, err = ReceiveGiftCode(giftCode.Code, 999, "")
	require.ErrorContains(t, err, "接收用户不存在")

	var refreshedGiftCode GiftCode
	require.NoError(t, db.First(&refreshedGiftCode, giftCode.Id).Error)
	require.Equal(t, GiftCodeStatusEnabled, refreshedGiftCode.Status)
	require.Zero(t, refreshedGiftCode.ReceivedUserId)
}
