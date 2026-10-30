package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// OAuth 绑定身份检查与 identityInUse 是同一类安全边界：必须回答"这个第三方
// 账号是否已经绑定过"，而不是"是否恰好查到一行"。计数失败时保守判为已占用，
// 否则数据库抖动的瞬间会把已绑定的账号当成新账号放行。

func newOAuthBindingTestDB(t *testing.T) {
	t.Helper()
	db := newIdentityTestDB(t)
	require.NoError(t, db.AutoMigrate(&UserOAuthBinding{}))
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })
}

func TestIsProviderUserIdTakenFailsClosedWhenCountFails(t *testing.T) {
	newOAuthBindingTestDB(t)
	require.NoError(t, DB.Migrator().DropTable(&UserOAuthBinding{}))

	require.True(t, IsProviderUserIdTaken(1, "subject-x"),
		"a failed count must not be reported as a free OAuth identity")
}

func TestIsProviderUserIdTakenReportsExistingBinding(t *testing.T) {
	newOAuthBindingTestDB(t)
	require.NoError(t, DB.Create(&UserOAuthBinding{
		UserId:         7,
		ProviderId:     1,
		ProviderUserId: "subject-7",
	}).Error)

	require.True(t, IsProviderUserIdTaken(1, "subject-7"))
	require.False(t, IsProviderUserIdTaken(1, "subject-other"))
	require.False(t, IsProviderUserIdTaken(2, "subject-7"), "provider id must be part of the identity")
}

func TestCreateUserOAuthBindingWithTxFailsClosedWhenCountFails(t *testing.T) {
	newOAuthBindingTestDB(t)
	tx := DB.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Migrator().DropTable(&UserOAuthBinding{}))
	t.Cleanup(func() { _ = tx.Rollback().Error })

	err := CreateUserOAuthBindingWithTx(tx, &UserOAuthBinding{
		UserId:         7,
		ProviderId:     1,
		ProviderUserId: "subject-7",
	})
	// 断言到"检查失败"这一步：表被删后插入本身也会失败，只看 require.Error
	// 会掩盖检查错误被吞掉这一事实。
	require.ErrorContains(t, err, "failed to check existing OAuth binding")
}
