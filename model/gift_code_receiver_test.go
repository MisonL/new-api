package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// NormalizeGiftCodeReceiver 把"绑定方式 + 绑定值"解析为一个具体账户。
// 解析结果里的 User.Id 会成为礼品码的 ReceiverUserId，而 ReceiveGiftCode
// 只允许该 id 领取，因此选错账户等于把额度发给另一个账号。
//
// 三种绑定方式里只有 email 不是唯一索引（username 是 unique，id 是主键）。
// 库里存在两行同邮箱记录时，First 会任意挑一行且不报错，这里必须 fail-closed。

func openGiftCodeTestDB(t *testing.T) {
	t.Helper()
	previous := DB
	DB = openTestDB(t, &User{})
	t.Cleanup(func() { DB = previous })
}

func TestNormalizeGiftCodeReceiverRejectsDuplicateEmailRows(t *testing.T) {
	openGiftCodeTestDB(t)
	createIdentityTestUser(t, DB, "dup-one", "dup@example.com")
	createIdentityTestUser(t, DB, "dup-two", "dup@example.com")

	var rows int64
	require.NoError(t, DB.Model(&User{}).Where("email = ?", "dup@example.com").Count(&rows).Error)
	require.Equal(t, int64(2), rows, "fixture precondition: two rows share the address")

	_, _, user, err := NormalizeGiftCodeReceiver(GiftCodeReceiverTypeEmail, "dup@example.com")

	require.Error(t, err, "同一邮箱关联多个账户时必须拒绝绑定，而不是任意挑一个")
	require.Nil(t, user)
}

func TestNormalizeGiftCodeReceiverAcceptsSingleEmailRow(t *testing.T) {
	openGiftCodeTestDB(t)
	createIdentityTestUser(t, DB, "solo", "solo@example.com")

	bindType, bindValue, user, err := NormalizeGiftCodeReceiver(GiftCodeReceiverTypeEmail, "solo@example.com")

	require.NoError(t, err)
	require.Equal(t, GiftCodeReceiverTypeEmail, bindType)
	require.Equal(t, "solo@example.com", bindValue)
	require.NotNil(t, user)
	require.Equal(t, "solo", user.Username)
}

func TestNormalizeGiftCodeReceiverIgnoresSoftDeletedRows(t *testing.T) {
	openGiftCodeTestDB(t)
	createIdentityTestUser(t, DB, "solo", "solo@example.com")
	createIdentityTestUser(t, DB, "tombstone", "solo@example.com")
	require.NoError(t, DB.Where("username = ?", "tombstone").Delete(&User{}).Error)

	_, _, user, err := NormalizeGiftCodeReceiver(GiftCodeReceiverTypeEmail, "solo@example.com")

	require.NoError(t, err, "墓碑行不应挡住唯一在用账户的绑定")
	require.NotNil(t, user)
	require.Equal(t, "solo", user.Username)
}

func TestNormalizeGiftCodeReceiverRejectsUnknownEmail(t *testing.T) {
	openGiftCodeTestDB(t)

	_, _, user, err := NormalizeGiftCodeReceiver(GiftCodeReceiverTypeEmail, "nobody@example.com")

	require.Error(t, err)
	require.Nil(t, user)
}
