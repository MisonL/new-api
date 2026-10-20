package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ChannelStatusActor identifies who drove a channel status change. AdminID 0
// with an empty username means the system (automatic disable/enable); a
// nonzero AdminID names the operator.
type ChannelStatusActor struct {
	AdminID       int
	AdminUsername string
	Source        string
}

// statusAuditLog writes one manage-log entry describing a channel status
// change. otherFieldName keys the audit payload inside admin_info so readers
// can distinguish status changes from other admin actions sharing the table.
func statusAuditLog(channelId int, oldStatus int, newStatus int, reason string, actor ChannelStatusActor, otherFieldName string, content string) {
	oldName := channelStatusName(oldStatus)
	newName := channelStatusName(newStatus)
	if reason == "" {
		reason = actor.Source
	}
	adminInfo := map[string]interface{}{
		otherFieldName: map[string]interface{}{
			"channel_id": channelId,
			"old_status": oldStatus,
			"new_status": newStatus,
			// String forms make the log readable without knowing the numeric
			// status constants.
			"old_status_n": oldName,
			"new_status_n": newName,
			"source":       actor.Source,
			"reason":       reason,
		},
		"admin_id":       actor.AdminID,
		"admin_username": actor.AdminUsername,
	}
	RecordLogWithAdminInfo(0, LogTypeManage, content, adminInfo)
}

// channelStatusName renders a status constant for audit logs. Unknown values
// are rendered numerically rather than guessed.
func channelStatusName(status int) string {
	switch status {
	case common.ChannelStatusEnabled:
		return "enabled"
	case common.ChannelStatusManuallyDisabled:
		return "manually_disabled"
	case common.ChannelStatusAutoDisabled:
		return "auto_disabled"
	case common.ChannelStatusUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("%d", status)
	}
}

// resolveAuditActor returns the actor with the username filled in: the caller's
// username when given, else a one-time lookup by admin id, else unchanged. The
// resolution runs once per write (not per log field), so batch paths pay for
// it once.
func resolveAuditActor(actor ChannelStatusActor) ChannelStatusActor {
	if actor.AdminUsername == "" && actor.AdminID != 0 {
		if resolved, err := GetUsernameById(actor.AdminID, false); err == nil && resolved != "" {
			actor.AdminUsername = resolved
		}
	}
	return actor
}

// actorLabel renders the actor for the log content line: the username for an
// operator, "system" for an automatic change (the content must not attribute
// an automatic change to an admin).
func actorLabel(actor ChannelStatusActor) string {
	if actor.AdminUsername != "" {
		return actor.AdminUsername
	}
	return "system"
}

// UpdateChannelStatusWithActor is UpdateChannelStatus plus a mandatory audit
// trail: a status change decides whether traffic reaches the channel, so every
// actual change records who did it. A no-op (status already equals the target)
// writes nothing and reports false, matching UpdateChannelStatus.
func UpdateChannelStatusWithActor(channelId int, usingKey string, status int, reason string, actor ChannelStatusActor) bool {
	channel, err := GetChannelById(channelId, true)
	if err != nil {
		return false
	}
	oldStatus := channel.Status
	ok := UpdateChannelStatus(channelId, usingKey, status, reason)
	if !ok {
		return false
	}
	// The content names the channel and the operator so a log reader can
	// attribute a change without decoding the JSON payload.
	actor = resolveAuditActor(actor)
	statusAuditLog(channelId, oldStatus, status, reason, actor, "channel_status_audit",
		fmt.Sprintf("渠道 %s（%d）状态变更：%s -> %s，操作者 %s，来源 %s，原因 %s",
			channel.Name, channelId, channelStatusName(oldStatus), channelStatusName(status),
			actorLabel(actor), actor.Source, reason))
	return true
}

// setChannelStatusByTag is the shared implementation of the tag-level status
// writes. The channel rows and the ability rows move inside one transaction:
// committing them separately leaves a half-state (channel disabled while its
// ability stays enabled) when the second write fails, and the status write is
// what takes the channel out of rotation. Audit entries are written only after
// the commit, one per channel whose status actually changed, so a rolled-back
// change leaves no entry claiming it happened. The actor username is resolved
// once for the whole batch.
func setChannelStatusByTag(tag string, status int, abilityEnabled bool, actor ChannelStatusActor, source string, contentFormat string) error {
	channels, err := GetChannelsByTag(tag, false, false)
	if err != nil {
		return err
	}
	actor = resolveAuditActor(actor)
	if actor.Source == "" {
		actor.Source = source
	}
	var changed []*Channel
	for _, channel := range channels {
		if channel.Status == status {
			continue
		}
		changed = append(changed, channel)
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Channel{}).Where("tag = ?", tag).Update("status", status).Error; err != nil {
			return err
		}
		return tx.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", abilityEnabled).Error
	})
	if err != nil {
		return err
	}
	for _, channel := range changed {
		statusAuditLog(channel.Id, channel.Status, status, "", actor, "channel_status_audit",
			fmt.Sprintf(contentFormat, channel.Name, channel.Id, tag, actorLabel(actor), actor.Source))
	}
	refreshChannelCacheAfterWrite()
	return nil
}

// DisableChannelByTagWithActor disables every channel under a tag in one
// transaction and audits each affected channel individually.
func DisableChannelByTagWithActor(tag string, actor ChannelStatusActor) error {
	return setChannelStatusByTag(tag, common.ChannelStatusManuallyDisabled, false, actor, "tag_disable",
		"渠道 %s（%d）随标签 %s 批量禁用，操作者 %s，来源 %s")
}

// EnableChannelByTagWithActor is the enable-side mirror of
// DisableChannelByTagWithActor: 启用同样改变流量走向，逐渠道留审计，
// 否则审计链可被"禁用有记录、启用无归因"半绕过。
func EnableChannelByTagWithActor(tag string, actor ChannelStatusActor) error {
	return setChannelStatusByTag(tag, common.ChannelStatusEnabled, true, actor, "tag_enable",
		"渠道 %s（%d）随标签 %s 批量启用，操作者 %s，来源 %s")
}
