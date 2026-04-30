package service

import (
	"fmt"
	"html"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

func NotifyGiftCodeReceived(giftCode *model.GiftCode) {
	if giftCode == nil || giftCode.CreatorUserId == 0 {
		return
	}
	creator, err := model.GetUserById(giftCode.CreatorUserId, true)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to query gift code creator %d: %s", giftCode.CreatorUserId, err.Error()))
		return
	}
	receiverName := giftCodeNotificationText(giftCode.ReceivedUsername)
	if receiverName == "" {
		receiverName = fmt.Sprintf("用户 %d", giftCode.ReceivedUserId)
	}
	thankMessage := giftCodeNotificationText(giftCode.ThankMessage)
	if thankMessage == "" {
		thankMessage = "对方没有填写回信。"
	}
	subject := "礼品码已被接收"
	content := fmt.Sprintf(
		"%s 已接收你的礼品码 %s，额度 %s。\n\n回信：%s",
		receiverName,
		giftCode.Code,
		logger.LogQuota(giftCode.Quota),
		thankMessage,
	)
	notification := dto.NewNotify(dto.NotifyTypeGiftCode, subject, content, nil)
	userSetting := creator.GetSetting()
	notifyType := userSetting.NotifyType
	if notifyType == "" {
		notifyType = dto.NotifyTypeEmail
	}
	emailToUse := userSetting.NotificationEmail
	if emailToUse == "" {
		emailToUse = creator.Email
	}
	if giftCodePrimaryNotifyAvailable(notifyType, userSetting, emailToUse) {
		notifyErr := NotifyUser(creator.Id, emailToUse, userSetting, notification)
		if notifyErr == nil {
			return
		}
		common.SysLog(fmt.Sprintf("failed to notify gift code creator %d: %s", creator.Id, notifyErr.Error()))
	} else {
		common.SysLog(fmt.Sprintf("gift code creator %d has no %s notification target", creator.Id, notifyType))
	}
	if notifyType == dto.NotifyTypeEmail || emailToUse == "" {
		return
	}
	emailFallbackSetting := userSetting
	emailFallbackSetting.NotifyType = dto.NotifyTypeEmail
	emailFallbackSetting.NotificationEmail = emailToUse
	if err := NotifyUser(creator.Id, creator.Email, emailFallbackSetting, notification); err != nil {
		common.SysLog(fmt.Sprintf("failed to email gift code creator %d: %s", creator.Id, err.Error()))
	}
}

func giftCodeNotificationText(value string) string {
	return html.EscapeString(strings.TrimSpace(value))
}

func giftCodePrimaryNotifyAvailable(notifyType string, userSetting dto.UserSetting, emailToUse string) bool {
	switch notifyType {
	case dto.NotifyTypeEmail:
		return strings.TrimSpace(emailToUse) != ""
	case dto.NotifyTypeWebhook:
		return strings.TrimSpace(userSetting.WebhookUrl) != ""
	case dto.NotifyTypeBark:
		return strings.TrimSpace(userSetting.BarkUrl) != ""
	case dto.NotifyTypeGotify:
		return strings.TrimSpace(userSetting.GotifyUrl) != "" &&
			strings.TrimSpace(userSetting.GotifyToken) != ""
	default:
		return false
	}
}
