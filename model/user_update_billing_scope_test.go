package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// User.Update 把调用方持有的 User 快照按非零字段整体写回。结算与邀请链路对
// quota / used_quota / request_count / aff_* 做的是 SQL 层相对更新
// （quota = quota - ?），两条路径并发时快照写回会把窗口内的相对扣减整段覆盖
// （第十轮 P1-2：陈旧快照把并发结算的 400 额度扣减改回原值，随后缓存再写一遍，
// 数据库与读路径同时失真）。
//
// 这些结算列必须从 Update 的写入集合里排除；其余字段保持"零值不写"的存量子集
// 语义——多处调用方持有部分结构体走 Update，改成全量白名单会把未提供的零值写进
// 数据库。

func newBillingScopeTestUser() *User {
	return &User{
		Id:           101,
		Username:     "quota-scope",
		Password:     "hashed-placeholder",
		Role:         common.RoleCommonUser,
		Status:       common.UserStatusEnabled,
		Quota:        1000,
		UsedQuota:    200,
		RequestCount: 3,
		AffQuota:     50,
		AffCount:     2,
		Group:        "default",
		AffCode:      "aff-quota-scope",
	}
}

func TestUpdateDoesNotClobberConcurrentQuotaSettle(t *testing.T) {
	db := openTestDB(t, &User{})
	DB = db

	user := newBillingScopeTestUser()
	require.NoError(t, db.Create(user).Error)

	// 请求 A：先读快照（例如改设置 / 改语言），尚未写回。
	snapshot, err := GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, 1000, snapshot.Quota)

	// 请求 B：并发结算做相对扣减与用量自增。
	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).
		Update("quota", gorm.Expr("quota - ?", 400)).Error)
	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).
		Update("used_quota", gorm.Expr("used_quota + ?", 400)).Error)

	// 请求 A 只改了自己的语言设置，随后把陈旧快照整体写回。
	snapshot.SetSetting(dto.UserSetting{Language: "zh"})
	require.NoError(t, snapshot.Update(false))

	final, err := GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, 600, final.Quota, "并发结算的相对扣减不能被陈旧快照覆盖")
	require.Equal(t, 600, final.UsedQuota, "并发用量自增不能被陈旧快照覆盖")

	var parsed dto.UserSetting
	require.NoError(t, common.UnmarshalJsonStr(final.Setting, &parsed))
	require.Equal(t, "zh", parsed.Language, "调用方真正修改的列必须落库")
}

// Update 仍要写调用方真正修改的普通列——上面的保护不能靠"整体不写"来实现。
func TestUpdateStillWritesCallerColumns(t *testing.T) {
	db := openTestDB(t, &User{})
	DB = db

	user := newBillingScopeTestUser()
	require.NoError(t, db.Create(user).Error)

	loaded, err := GetUserById(user.Id, false)
	require.NoError(t, err)
	loaded.Status = common.UserStatusDisabled
	loaded.Remark = "reviewed"
	loaded.Email = "reviewed@example.com"
	require.NoError(t, loaded.Update(false))

	final, err := GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusDisabled, final.Status)
	require.Equal(t, "reviewed", final.Remark)
	require.Equal(t, "reviewed@example.com", final.Email)
	// 未被修改的结算列保持原值。
	require.Equal(t, 1000, final.Quota)
	require.Equal(t, 200, final.UsedQuota)
}
