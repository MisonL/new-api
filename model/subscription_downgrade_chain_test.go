package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 链式升级：VIP 有效期内购买 SVIP 时，SVIP 的 PrevUserGroup 记录的是 VIP。
// 两级订阅都过期后只回退到 SVIP 的 prev 会让用户永久保留 VIP 分组。
// 期望：沿 PrevUserGroup 继续回退，直到目标分组不再是已过期订阅的升级目标。

func setupSubscriptionDowngradeTest(t *testing.T) {
	t.Helper()
	db := openTestDB(t, &User{}, &UserSubscription{}, &SubscriptionPlan{})
	previousDB := DB
	DB = db
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.RedisEnabled = previousRedis
	})
}

func seedDowngradeUser(t *testing.T, id int, group string) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id:       id,
		Username: "downgrade-user",
		Group:    group,
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
	}).Error)
}

func seedExpiredSubscription(t *testing.T, id, userId int, upgradeGroup, prevGroup string, endTime int64) {
	t.Helper()
	require.NoError(t, DB.Create(&UserSubscription{
		Id:            id,
		UserId:        userId,
		PlanId:        1,
		Status:        "expired",
		AmountTotal:   1000,
		UpgradeGroup:  upgradeGroup,
		PrevUserGroup: prevGroup,
		StartTime:     endTime - 86400,
		EndTime:       endTime,
	}).Error)
}

func seedDueSubscription(t *testing.T, id, userId int, upgradeGroup, prevGroup string, endTime int64) {
	t.Helper()
	require.NoError(t, DB.Create(&UserSubscription{
		Id:            id,
		UserId:        userId,
		PlanId:        1,
		Status:        "active",
		AmountTotal:   1000,
		UpgradeGroup:  upgradeGroup,
		PrevUserGroup: prevGroup,
		StartTime:     endTime - 86400,
		EndTime:       endTime,
	}).Error)
}

func userGroup(t *testing.T, userId int) string {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	return user.Group
}

func TestExpireDueSubscriptionsUnwindsChainedUpgradeGroups(t *testing.T) {
	setupSubscriptionDowngradeTest(t)
	now := common.GetTimestamp()

	seedDowngradeUser(t, 9301, "svip")
	// VIP 早已过期（prev=default），SVIP 当前到期（prev=VIP）。
	seedExpiredSubscription(t, 93011, 9301, "vip", "default", now-7200)
	seedDueSubscription(t, 93012, 9301, "svip", "vip", now-60)

	_, err := ExpireDueSubscriptions(100)
	require.NoError(t, err)
	require.Equal(t, "default", userGroup(t, 9301),
		"整条升级链过期后必须回退到链首之前的分组，而不是中间档")
}

func TestExpireDueSubscriptionsKeepsGroupWhenAnotherUpgradeStillActive(t *testing.T) {
	setupSubscriptionDowngradeTest(t)
	now := common.GetTimestamp()

	seedDowngradeUser(t, 9302, "svip")
	seedDueSubscription(t, 93021, 9302, "svip", "vip", now-60)
	// 另一个仍然有效的升级订阅（end_time 在未来）应阻止回退。
	require.NoError(t, DB.Create(&UserSubscription{
		Id: 93022, UserId: 9302, PlanId: 1, Status: "active",
		AmountTotal: 1000, UpgradeGroup: "enterprise", PrevUserGroup: "svip",
		StartTime: now - 60, EndTime: now + 86400,
	}).Error)

	_, err := ExpireDueSubscriptions(100)
	require.NoError(t, err)
	require.Equal(t, "svip", userGroup(t, 9302))
}

func TestExpireDueSubscriptionsSingleLevelStillReverts(t *testing.T) {
	setupSubscriptionDowngradeTest(t)
	now := common.GetTimestamp()

	seedDowngradeUser(t, 9303, "vip")
	seedDueSubscription(t, 93031, 9303, "vip", "default", now-60)

	_, err := ExpireDueSubscriptions(100)
	require.NoError(t, err)
	require.Equal(t, "default", userGroup(t, 9303))
}

func TestExpireDueSubscriptionsDoesNotRevertManuallyChangedGroup(t *testing.T) {
	setupSubscriptionDowngradeTest(t)
	now := common.GetTimestamp()

	// 用户当前分组已被管理员改成 gold，与订阅记录的升级目标 vip 不一致。
	seedDowngradeUser(t, 9304, "gold")
	seedDueSubscription(t, 93041, 9304, "vip", "default", now-60)

	_, err := ExpireDueSubscriptions(100)
	require.NoError(t, err)
	require.Equal(t, "gold", userGroup(t, 9304),
		"当前分组不是该订阅授予的升级分组时不能回退")
}
