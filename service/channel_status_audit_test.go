package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// The automatic disable path is the one that changes a channel's status without
// an operator asking for it, so an audit reader has to be able to tell it apart
// from a deliberate change. This drives service.DisableChannel itself rather
// than the model function, because the source string is chosen here.
func TestDisableChannelAuditsTheAutomaticDisable(t *testing.T) {
	const id = 93001
	priority := int64(10)
	// The status write moves the channel row and its ability rows in one
	// transaction, and the shared test database does not migrate abilities.
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
	require.NoError(t, model.DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM logs").Error)
	// The shared database is reused by every test in this package and the
	// management-log table is shared with them, so the rows this test writes are
	// removed when it ends. Tests that assert an exact log count would otherwise
	// see this test's audit entry whenever the order puts it first.
	t.Cleanup(func() {
		_ = model.LOG_DB.Where("type = ?", model.LogTypeManage).Delete(&model.Log{}).Error
		_ = model.DB.Exec("DELETE FROM channels").Error
		_ = model.DB.Exec("DELETE FROM abilities").Error
	})
	require.NoError(t, model.DB.Create(&model.Channel{
		Id: id, Name: "service-auto-ban", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "service-auto-ban-model",
		Priority: &priority, Weight: common.GetPointer(uint(1)),
	}).Error)

	DisableChannel(types.ChannelError{
		ChannelId:   id,
		ChannelName: "service-auto-ban",
		AutoBan:     true,
	}, "upstream returned 502")

	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, id).Error)
	require.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)

	var entries []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeManage).Find(&entries).Error)
	require.Len(t, entries, 1, "the automatic disable must be audited")

	other, err := common.StrToMap(entries[0].Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	audit, ok := adminInfo["channel_status_audit"].(map[string]interface{})
	require.True(t, ok)

	require.Equal(t, "auto_ban", audit["source"],
		"an automatic disable must be attributed to the system, not to an admin")
	require.EqualValues(t, 0, adminInfo["admin_id"])
	require.Equal(t, "", adminInfo["admin_username"])
	require.EqualValues(t, common.ChannelStatusAutoDisabled, audit["new_status"])
	require.Equal(t, "upstream returned 502", audit["reason"])
}
