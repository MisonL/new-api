package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// prepareStatusAuditTest resets the shared tables. prepareChannelCacheTest
// already removes the management-log rows a status change writes, which is what
// keeps these cases from leaking entries into the tests that count them.
func prepareStatusAuditTest(t *testing.T) {
	t.Helper()
	prepareChannelCacheTest(t)
}

func seedAuditChannel(t *testing.T, id int, status int) *Channel {
	t.Helper()
	priority := int64(10)
	channel := &Channel{
		Id: id, Name: "status-audit", Status: status,
		Group: "default", Models: "status-audit-model",
		Priority: &priority, Weight: common.GetPointer(uint(1)),
	}
	require.NoError(t, DB.Create(channel).Error)
	return channel
}

// auditEntriesForChannel returns the status-audit entries naming one channel.
// Scoped by channel id rather than "the last row", because the management log
// table is shared across tests and other cases may have written entries.
func auditEntriesForChannel(t *testing.T, channelId int) []map[string]interface{} {
	t.Helper()
	var entries []Log
	require.NoError(t, LOG_DB.Where("type = ?", LogTypeManage).Find(&entries).Error)
	matched := make([]map[string]interface{}, 0, 1)
	for _, entry := range entries {
		other, err := common.StrToMap(entry.Other)
		if err != nil {
			continue
		}
		adminInfo, ok := other["admin_info"].(map[string]interface{})
		if !ok {
			continue
		}
		audit, ok := adminInfo["channel_status_audit"].(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := audit["channel_id"].(float64); ok && int(id) == channelId {
			audit["_content"] = entry.Content
			// admin_id / admin_username live in the enclosing admin_info, which
			// is where RecordLogWithAdminInfo puts them.
			audit["_admin_id"] = adminInfo["admin_id"]
			audit["_admin_username"] = adminInfo["admin_username"]
			matched = append(matched, audit)
		}
	}
	return matched
}

func singleStatusAudit(t *testing.T, channelId int) map[string]interface{} {
	t.Helper()
	matched := auditEntriesForChannel(t, channelId)
	require.Len(t, matched, 1, "expected exactly one audit entry for channel %d", channelId)
	return matched[0]
}

// A channel status change decides whether traffic reaches the channel, so it
// must leave a trace naming the channel, both statuses, and who did it.
func TestChannelStatusChangeIsAuditedWithActor(t *testing.T) {
	prepareStatusAuditTest(t)

	seedAuditChannel(t, 97001, common.ChannelStatusEnabled)

	ok := UpdateChannelStatusWithActor(97001, "", common.ChannelStatusManuallyDisabled, "operator decision",
		ChannelStatusActor{AdminID: 42, AdminUsername: "audit-admin", Source: "channel_update"})
	require.True(t, ok)

	audit := singleStatusAudit(t, 97001)
	content := audit["_content"].(string)
	require.Contains(t, content, "status-audit")
	require.Contains(t, content, "audit-admin")
	require.EqualValues(t, 97001, audit["channel_id"])
	require.EqualValues(t, common.ChannelStatusEnabled, audit["old_status"])
	require.EqualValues(t, common.ChannelStatusManuallyDisabled, audit["new_status"])
	require.Equal(t, "enabled", audit["old_status_n"])
	require.Equal(t, "manually_disabled", audit["new_status_n"])
	require.Equal(t, "channel_update", audit["source"])
	require.Equal(t, "operator decision", audit["reason"])
	require.EqualValues(t, 42, audit["_admin_id"])
	require.Equal(t, "audit-admin", audit["_admin_username"])
}

// An automatic disable must be recorded too, attributed to the system rather
// than to an admin, so an unexplained status can be told apart from an
// operator's deliberate change.
func TestAutomaticChannelStatusChangeIsAuditedAsSystem(t *testing.T) {
	prepareStatusAuditTest(t)

	seedAuditChannel(t, 97002, common.ChannelStatusEnabled)

	ok := UpdateChannelStatusWithActor(97002, "", common.ChannelStatusAutoDisabled, "upstream 502",
		ChannelStatusActor{Source: "auto_ban"})
	require.True(t, ok)

	audit := singleStatusAudit(t, 97002)
	content := audit["_content"].(string)
	require.Equal(t, "auto_ban", audit["source"])
	require.EqualValues(t, 0, audit["_admin_id"],
		"an automatic change carries no admin id")
	require.Equal(t, "", audit["_admin_username"],
		"an automatic change carries no admin username")
	require.NotContains(t, content, "管理员", "an automatic change must not be attributed to an admin")
}

// A request that does not change the status must not add an audit entry, or the
// log fills with no-ops and the real changes become hard to find.
func TestNoOpStatusChangeIsNotAudited(t *testing.T) {
	prepareStatusAuditTest(t)

	seedAuditChannel(t, 97003, common.ChannelStatusEnabled)

	ok := UpdateChannelStatusWithActor(97003, "", common.ChannelStatusEnabled, "",
		ChannelStatusActor{AdminID: 42, AdminUsername: "audit-admin", Source: "channel_update"})
	require.False(t, ok, "a no-op status write reports no change")
	require.Empty(t, auditEntriesForChannel(t, 97003),
		"a no-op must not add an audit entry")
}

// A tag covers many channels; each one that changes must get its own entry, so
// an audit reader asking about a single channel finds its id.
func TestTagStatusChangeAuditsEveryAffectedChannel(t *testing.T) {
	prepareStatusAuditTest(t)

	tag := "status-audit-tag"
	priority := int64(10)
	for _, id := range []int{97004, 97005} {
		require.NoError(t, DB.Create(&Channel{
			Id: id, Name: "status-audit-tag", Status: common.ChannelStatusEnabled,
			Group: "default", Models: "status-audit-model", Tag: &tag,
			Priority: &priority, Weight: common.GetPointer(uint(1)),
		}).Error)
		require.NoError(t, DB.Create(&Ability{
			Group: "default", Model: "status-audit-model", ChannelId: id,
			Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
		}).Error)
	}

	require.NoError(t, DisableChannelByTagWithActor(tag,
		ChannelStatusActor{AdminID: 42, AdminUsername: "audit-admin", Source: "tag_disable"}))

	for _, id := range []int{97004, 97005} {
		audit := singleStatusAudit(t, id)
		require.Equal(t, "tag_disable", audit["source"])
		require.Equal(t, "audit-admin", audit["_admin_username"])
	}
}

// A tag can cover hundreds of channels, so the audit entries go in as few
// statements as possible. This pins the batching boundary: 250 channels must
// produce 250 entries, which only holds if every chunk is written.
func TestTagStatusChangeAuditsEveryChannelAcrossChunks(t *testing.T) {
	prepareStatusAuditTest(t)

	tag := "status-audit-chunked"
	priority := int64(10)
	const count = 250
	ids := make([]int, 0, count)
	for i := 0; i < count; i++ {
		id := 97100 + i
		ids = append(ids, id)
		require.NoError(t, DB.Create(&Channel{
			Id: id, Name: "status-audit-chunked", Status: common.ChannelStatusEnabled,
			Group: "default", Models: "status-audit-chunked-model", Tag: &tag,
			Priority: &priority, Weight: common.GetPointer(uint(1)),
		}).Error)
	}

	require.NoError(t, DisableChannelByTagWithActor(tag,
		ChannelStatusActor{AdminID: 42, AdminUsername: "audit-admin", Source: "tag_disable"}))

	var entries []Log
	require.NoError(t, LOG_DB.Where("type = ?", LogTypeManage).Find(&entries).Error)
	// Compared as a count rather than with require.Len: a failure here means
	// hundreds of rows are missing, and require.Len would print every one of the
	// rows that did land.
	require.Equal(t, count, len(entries),
		"every affected channel must be audited; a dropped batch would silently lose entries")

	// Each entry must name a distinct channel: a batching bug that reused the
	// last entry would still produce the right count but no usable audit.
	seen := make(map[int]bool, count)
	for _, entry := range entries {
		other, err := common.StrToMap(entry.Other)
		require.NoError(t, err)
		adminInfo := other["admin_info"].(map[string]interface{})
		audit := adminInfo["channel_status_audit"].(map[string]interface{})
		seen[int(audit["channel_id"].(float64))] = true
	}
	require.Len(t, seen, count, "each entry must name its own channel")
	for _, id := range ids {
		require.True(t, seen[id], "channel %d has no audit entry", id)
	}
}

// An admin id without a username must still produce a readable entry. The
// username is resolved once for the whole batch rather than per channel, so
// this pins that the resolution actually happens.
func TestStatusAuditResolvesUsernameFromAdminID(t *testing.T) {
	prepareStatusAuditTest(t)

	require.NoError(t, DB.Create(&User{
		Id: 98001, Username: "resolved-admin", Password: "password123",
		Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default",
		// An explicit code: users created through DB.Create bypass the
		// Insert path that generates one, and several tests in this package
		// create users without a code, so leaving this row's code empty makes
		// the next such insert fail the unique index.
		AffCode: "resolved-admin-aff",
	}).Error)
	t.Cleanup(func() {
		_ = DB.Where("id = ?", 98001).Delete(&User{}).Error
	})
	seedAuditChannel(t, 98002, common.ChannelStatusEnabled)

	require.True(t, UpdateChannelStatusWithActor(98002, "", common.ChannelStatusManuallyDisabled, "by id only",
		ChannelStatusActor{AdminID: 98001, Source: "channel_update"}))

	audit := singleStatusAudit(t, 98002)
	require.Contains(t, audit["_content"], "resolved-admin",
		"the username must be resolved from the admin id, not left blank")
	require.Equal(t, "resolved-admin", audit["_admin_username"])
}
