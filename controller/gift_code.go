package controller

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type CreateGiftCodeRequest struct {
	Quota             int    `json:"quota"`
	ReceiverBindType  string `json:"receiver_bind_type"`
	ReceiverBindValue string `json:"receiver_bind_value"`
	Message           string `json:"message"`
	ExpiredTime       int64  `json:"expired_time"`
}

type ReceiveGiftCodeRequest struct {
	ThankMessage string `json:"thank_message"`
}

func ListGiftCodes(c *gin.Context) {
	userId := c.GetInt("id")
	giftCodes, err := model.ListGiftCodesByCreator(userId, 20)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, giftCodes)
}

func CreateGiftCode(c *gin.Context) {
	userId := c.GetInt("id")
	var req CreateGiftCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	req.ReceiverBindType = strings.TrimSpace(req.ReceiverBindType)
	req.ReceiverBindValue = strings.TrimSpace(req.ReceiverBindValue)
	req.Message = strings.TrimSpace(req.Message)
	if req.Quota <= 0 {
		common.ApiErrorMsg(c, "请输入礼品码额度")
		return
	}
	if float64(req.Quota) < common.QuotaPerUnit {
		common.ApiErrorMsg(c, "礼品码额度不能低于最低划转额度")
		return
	}
	if utf8.RuneCountInString(req.Message) > 500 {
		common.ApiErrorMsg(c, "礼品码留言不能超过 500 个字符")
		return
	}
	if utf8.RuneCountInString(req.ReceiverBindValue) > 128 {
		common.ApiErrorMsg(c, "绑定用户内容不能超过 128 个字符")
		return
	}
	if valid, msg := validateExpiredTime(c, req.ExpiredTime); !valid {
		common.ApiErrorMsg(c, msg)
		return
	}
	bindType, bindValue, receiver, err := model.NormalizeGiftCodeReceiver(req.ReceiverBindType, req.ReceiverBindValue)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	receiverUserId := 0
	if receiver != nil {
		receiverUserId = receiver.Id
		if receiverUserId == userId {
			common.ApiErrorMsg(c, "不能给自己生成礼品码")
			return
		}
	}
	giftCode, err := model.CreateGiftCode(userId, req.Quota, receiverUserId, bindType, bindValue, req.Message, req.ExpiredTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, giftCode)
}

func GetGiftCode(c *gin.Context) {
	userId := c.GetInt("id")
	giftCode, err := model.GetGiftCodeByCode(c.Param("code"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !canViewGiftCode(giftCode, userId) {
		common.ApiErrorMsg(c, "该礼品码已绑定其他用户")
		return
	}
	common.ApiSuccess(c, sanitizeGiftCodeForUser(giftCode, userId))
}

func ReceiveGiftCode(c *gin.Context) {
	userId := c.GetInt("id")
	var req ReceiveGiftCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	req.ThankMessage = strings.TrimSpace(req.ThankMessage)
	if utf8.RuneCountInString(req.ThankMessage) > 500 {
		common.ApiErrorMsg(c, "感谢回信不能超过 500 个字符")
		return
	}
	giftCode, err := model.ReceiveGiftCode(c.Param("code"), userId, req.ThankMessage)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	go func(giftCode *model.GiftCode) {
		defer func() {
			if err := recover(); err != nil {
				common.SysLog(fmt.Sprintf("failed to notify gift code creator: %v", err))
			}
		}()
		service.NotifyGiftCodeReceived(giftCode)
	}(giftCode)
	common.ApiSuccess(c, sanitizeGiftCodeForUser(giftCode, userId))
}

func sanitizeGiftCodeForUser(giftCode *model.GiftCode, userId int) *model.GiftCode {
	if giftCode == nil {
		return nil
	}
	clean := *giftCode
	if clean.CreatorUserId != userId {
		clean.ReceiverBindValue = ""
		clean.ThankMessage = ""
	}
	return &clean
}

func canViewGiftCode(giftCode *model.GiftCode, userId int) bool {
	if giftCode == nil {
		return false
	}
	if giftCode.CreatorUserId == userId || giftCode.ReceivedUserId == userId {
		return true
	}
	if giftCode.ReceivedUserId != 0 {
		return false
	}
	return giftCode.ReceiverUserId == 0 || giftCode.ReceiverUserId == userId
}
