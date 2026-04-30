package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInviteRebateTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	// This helper swaps package globals; do not call it from t.Parallel tests.
	db := openTestDB(t, &User{}, &TopUp{}, &Log{}, &InvitationRecord{})
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

func TestRechargeWaffoAccruesInviteRebateOnce(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	previousQuotaPerUnit := common.QuotaPerUnit
	previousInviteRebateRate := common.InviteRebateRate
	common.QuotaPerUnit = 1000
	common.InviteRebateRate = 20
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		common.InviteRebateRate = previousInviteRebateRate
	})

	inviter := User{Id: 1, Username: "inviter", Password: "Password123!", AffCode: "inviter-aff", Status: common.UserStatusEnabled}
	invitee := User{Id: 2, Username: "invitee", Password: "Password123!", AffCode: "invitee-aff", InviterId: inviter.Id, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&invitee).Error)
	require.NoError(t, db.Create(&TopUp{
		UserId:        invitee.Id,
		Amount:        10,
		Money:         10,
		TradeNo:       "waffo-rebate-once",
		PaymentMethod: "waffo",
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}).Error)

	require.NoError(t, RechargeWaffo("waffo-rebate-once", "127.0.0.1"))
	require.NoError(t, RechargeWaffo("waffo-rebate-once", "127.0.0.1"))

	var refreshedInviter User
	require.NoError(t, db.First(&refreshedInviter, inviter.Id).Error)
	require.Equal(t, 2000, refreshedInviter.AffQuota)
	require.Equal(t, 2000, refreshedInviter.AffHistoryQuota)

	var refreshedInvitee User
	require.NoError(t, db.First(&refreshedInvitee, invitee.Id).Error)
	require.Equal(t, 10000, refreshedInvitee.Quota)

	var records []InvitationRecord
	require.NoError(t, db.Order("id asc").Find(&records).Error)
	require.Len(t, records, 1)
	require.Equal(t, inviter.Id, records[0].InviterId)
	require.Equal(t, invitee.Id, records[0].InviteeId)
	require.Equal(t, InvitationRecordTypeTopUpRebate, records[0].RecordType)
	require.Equal(t, "waffo", records[0].Source)
	require.Equal(t, "waffo-rebate-once", records[0].SourceRef)
	require.Equal(t, 10000, records[0].TopUpQuota)
	require.Equal(t, 2000, records[0].RewardQuota)
	require.Equal(t, 20.0, records[0].RebateRate)
}

func TestRechargeEpayAccruesInviteRebate(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	previousQuotaPerUnit := common.QuotaPerUnit
	previousInviteRebateRate := common.InviteRebateRate
	common.QuotaPerUnit = 1000
	common.InviteRebateRate = 20
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		common.InviteRebateRate = previousInviteRebateRate
	})

	inviter := User{Id: 1, Username: "inviter", Password: "Password123!", AffCode: "inviter-aff", Status: common.UserStatusEnabled}
	invitee := User{Id: 2, Username: "invitee", Password: "Password123!", AffCode: "invitee-aff", InviterId: inviter.Id, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&invitee).Error)
	require.NoError(t, db.Create(&TopUp{
		UserId:        invitee.Id,
		Amount:        10,
		Money:         10,
		TradeNo:       "epay-rebate",
		PaymentMethod: "alipay",
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}).Error)

	require.NoError(t, RechargeEpay("epay-rebate", "127.0.0.1"))
	require.NoError(t, RechargeEpay("epay-rebate", "127.0.0.1"))

	var refreshedInviter User
	require.NoError(t, db.First(&refreshedInviter, inviter.Id).Error)
	require.Equal(t, 2000, refreshedInviter.AffQuota)
	require.Equal(t, 2000, refreshedInviter.AffHistoryQuota)

	var refreshedInvitee User
	require.NoError(t, db.First(&refreshedInvitee, invitee.Id).Error)
	require.Equal(t, 10000, refreshedInvitee.Quota)

	var records []InvitationRecord
	require.NoError(t, db.Order("id asc").Find(&records).Error)
	require.Len(t, records, 1)
	require.Equal(t, InvitationRecordTypeTopUpRebate, records[0].RecordType)
	require.Equal(t, "alipay", records[0].Source)
	require.Equal(t, "epay-rebate", records[0].SourceRef)
	require.Equal(t, 10000, records[0].TopUpQuota)
	require.Equal(t, 2000, records[0].RewardQuota)
}

func TestRewardInviterForTopUpSkipsUserWithoutInviter(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	previousInviteRebateRate := common.InviteRebateRate
	common.InviteRebateRate = 20
	t.Cleanup(func() {
		common.InviteRebateRate = previousInviteRebateRate
	})

	user := User{Id: 1, Username: "standalone", Password: "Password123!", AffCode: "standalone-aff", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		inviterId, rebateQuota, err := RewardInviterForTopUp(tx, user.Id, 10000)
		require.Zero(t, inviterId)
		require.Zero(t, rebateQuota)
		return err
	})
	require.NoError(t, err)
}

func TestRewardInviterForTopUpRejectsInvalidInviteeId(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	previousInviteRebateRate := common.InviteRebateRate
	common.InviteRebateRate = 20
	t.Cleanup(func() {
		common.InviteRebateRate = previousInviteRebateRate
	})

	err := db.Transaction(func(tx *gorm.DB) error {
		inviterId, rebateQuota, err := RewardInviterForTopUp(tx, -1, 10000)
		require.Zero(t, inviterId)
		require.Zero(t, rebateQuota)
		return err
	})
	require.ErrorContains(t, err, "被邀请用户无效")
}

func TestRewardInviterForTopUpRejectsMissingInvitee(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	previousInviteRebateRate := common.InviteRebateRate
	common.InviteRebateRate = 20
	t.Cleanup(func() {
		common.InviteRebateRate = previousInviteRebateRate
	})

	err := db.Transaction(func(tx *gorm.DB) error {
		inviterId, rebateQuota, err := RewardInviterForTopUp(tx, 101, 10000)
		require.Zero(t, inviterId)
		require.Zero(t, rebateQuota)
		return err
	})
	require.Error(t, err)
}

func TestInviteRebateRateOptionValidation(t *testing.T) {
	require.NoError(t, validateOptionValue("InviteRebateRate", "20"))
	require.NoError(t, validateOptionValue("InviteRebateRate", "0"))
	require.NoError(t, validateOptionValue("InviteRebateRate", "100"))
	require.Error(t, validateOptionValue("InviteRebateRate", "-0.1"))
	require.Error(t, validateOptionValue("InviteRebateRate", "100.1"))
	require.Error(t, validateOptionValue("InviteRebateRate", "NaN"))
	require.Error(t, validateOptionValue("InviteRebateRate", "abc"))
}

func TestListInvitationRecordsFiltersByKeywordAndType(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	inviter := User{Id: 1, Username: "inviter", Password: "Password123!", AffCode: "inviter-aff", Status: common.UserStatusEnabled}
	invitee := User{Id: 2, Username: "alice", Password: "Password123!", Email: "alice@example.com", AffCode: "alice-aff", InviterId: inviter.Id, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&invitee).Error)
	require.NoError(t, db.Create(&InvitationRecord{
		InviterId:   inviter.Id,
		InviteeId:   invitee.Id,
		RecordType:  InvitationRecordTypeTopUpRebate,
		Source:      "stripe",
		SourceRef:   "trade-no",
		TopUpQuota:  1000,
		RewardQuota: 100,
		RebateRate:  10,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	records, total, err := ListInvitationRecords(inviter.Id, InvitationRecordFilter{
		RecordType: InvitationRecordTypeTopUpRebate,
		Keyword:    "ali",
	}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	require.Equal(t, "alice", records[0].InviteeUsername)
}

func TestListInvitationRecordsIncludesLegacyInvitedUsers(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	previousQuotaForInviter := common.QuotaForInviter
	common.QuotaForInviter = 500
	t.Cleanup(func() {
		common.QuotaForInviter = previousQuotaForInviter
	})

	inviter := User{Id: 1, Username: "inviter", Password: "Password123!", AffCode: "inviter-aff", Status: common.UserStatusEnabled}
	invitee := User{Id: 2, Username: "legacy", Password: "Password123!", Email: "legacy@example.com", AffCode: "legacy-aff", InviterId: inviter.Id, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&invitee).Error)

	records, total, err := ListInvitationRecords(inviter.Id, InvitationRecordFilter{Source: "register"}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	require.Equal(t, InvitationRecordTypeRegister, records[0].RecordType)
	require.Equal(t, "register", records[0].Source)
	require.Equal(t, invitee.Id, records[0].InviteeId)
	require.Equal(t, "legacy", records[0].InviteeUsername)
	require.Equal(t, 500, records[0].RewardQuota)
}

func TestListInvitationRecordsAvoidsDuplicateRegisterRecord(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	inviter := User{Id: 1, Username: "inviter", Password: "Password123!", AffCode: "inviter-aff", Status: common.UserStatusEnabled}
	invitee := User{Id: 2, Username: "new-user", Password: "Password123!", Email: "new@example.com", AffCode: "new-aff", InviterId: inviter.Id, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&invitee).Error)
	require.NoError(t, db.Create(&InvitationRecord{
		InviterId:   inviter.Id,
		InviteeId:   invitee.Id,
		RecordType:  InvitationRecordTypeRegister,
		Source:      "register",
		RewardQuota: 100,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	records, total, err := ListInvitationRecords(inviter.Id, InvitationRecordFilter{}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	require.Equal(t, 100, records[0].RewardQuota)
}

func TestListInvitationInviteeStatsAggregatesFilteredRecords(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	previousQuotaForInviter := common.QuotaForInviter
	common.QuotaForInviter = 500
	t.Cleanup(func() {
		common.QuotaForInviter = previousQuotaForInviter
	})

	inviter := User{Id: 1, Username: "inviter", Password: "Password123!", AffCode: "inviter-aff", Status: common.UserStatusEnabled}
	alice := User{Id: 2, Username: "alice", Password: "Password123!", Email: "alice@example.com", AffCode: "alice-aff", InviterId: inviter.Id, Status: common.UserStatusEnabled}
	bob := User{Id: 3, Username: "bob", Password: "Password123!", Email: "bob@example.com", AffCode: "bob-aff", InviterId: inviter.Id, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	require.NoError(t, db.Create(&InvitationRecord{
		InviterId:   inviter.Id,
		InviteeId:   alice.Id,
		RecordType:  InvitationRecordTypeTopUpRebate,
		Source:      "stripe",
		TopUpQuota:  10000,
		RewardQuota: 2000,
		RebateRate:  20,
		CreatedTime: 20,
	}).Error)
	require.NoError(t, db.Create(&InvitationRecord{
		InviterId:   inviter.Id,
		InviteeId:   alice.Id,
		RecordType:  InvitationRecordTypeRegister,
		Source:      "register",
		RewardQuota: 100,
		CreatedTime: 10,
	}).Error)

	stats, err := ListInvitationInviteeStats(inviter.Id, InvitationRecordFilter{}, 8)
	require.NoError(t, err)
	require.Len(t, stats, 2)
	require.Equal(t, alice.Id, stats[0].InviteeId)
	require.Equal(t, 2100, stats[0].RewardQuota)
	require.Equal(t, 10000, stats[0].TopUpQuota)
	require.Equal(t, 1, stats[0].RegisterCount)
	require.Equal(t, 1, stats[0].RebateCount)
	require.Equal(t, bob.Id, stats[1].InviteeId)
	require.Equal(t, 500, stats[1].RewardQuota)
	require.Equal(t, 1, stats[1].RegisterCount)
}

func TestListInvitationInviteeStatsHonorsFilterAndLimit(t *testing.T) {
	db := setupInviteRebateTestDB(t)
	inviter := User{Id: 1, Username: "inviter", Password: "Password123!", AffCode: "inviter-aff", Status: common.UserStatusEnabled}
	alice := User{Id: 2, Username: "alice", Password: "Password123!", Email: "alice@example.com", AffCode: "alice-aff", InviterId: inviter.Id, Status: common.UserStatusEnabled}
	bob := User{Id: 3, Username: "bob", Password: "Password123!", Email: "bob@example.com", AffCode: "bob-aff", InviterId: inviter.Id, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&inviter).Error)
	require.NoError(t, db.Create(&alice).Error)
	require.NoError(t, db.Create(&bob).Error)
	require.NoError(t, db.Create(&InvitationRecord{
		InviterId:   inviter.Id,
		InviteeId:   alice.Id,
		RecordType:  InvitationRecordTypeTopUpRebate,
		Source:      "stripe",
		TopUpQuota:  10000,
		RewardQuota: 1000,
		RebateRate:  10,
		CreatedTime: 10,
	}).Error)
	require.NoError(t, db.Create(&InvitationRecord{
		InviterId:   inviter.Id,
		InviteeId:   bob.Id,
		RecordType:  InvitationRecordTypeTopUpRebate,
		Source:      "creem",
		TopUpQuota:  20000,
		RewardQuota: 3000,
		RebateRate:  15,
		CreatedTime: 20,
	}).Error)

	stats, err := ListInvitationInviteeStats(inviter.Id, InvitationRecordFilter{
		RecordType: InvitationRecordTypeTopUpRebate,
	}, 1)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	require.Equal(t, bob.Id, stats[0].InviteeId)
	require.Equal(t, 3000, stats[0].RewardQuota)
	require.Equal(t, 1, stats[0].RebateCount)
}
