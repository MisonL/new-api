package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	GiftCodeStatusEnabled  = 1
	GiftCodeStatusDisabled = 2
	GiftCodeStatusUsed     = 3
)

const (
	GiftCodeReceiverTypeUserId   = "id"
	GiftCodeReceiverTypeUsername = "username"
	GiftCodeReceiverTypeEmail    = "email"
)

type GiftCode struct {
	Id                int            `json:"id"`
	Code              string         `json:"code" gorm:"type:char(32);uniqueIndex"`
	CreatorUserId     int            `json:"creator_user_id" gorm:"index"`
	CreatorUsername   string         `json:"creator_username" gorm:"-:all"`
	ReceiverUserId    int            `json:"receiver_user_id" gorm:"index"`
	ReceiverBindType  string         `json:"receiver_bind_type" gorm:"type:varchar(16)"`
	ReceiverBindValue string         `json:"receiver_bind_value" gorm:"type:varchar(128)"`
	ReceivedUserId    int            `json:"received_user_id" gorm:"index"`
	ReceivedUsername  string         `json:"received_username" gorm:"-:all"`
	Status            int            `json:"status" gorm:"default:1;index"`
	Quota             int            `json:"quota" gorm:"default:0"`
	Message           string         `json:"message" gorm:"type:text"`
	ThankMessage      string         `json:"thank_message" gorm:"type:text"`
	CreatedTime       int64          `json:"created_time" gorm:"type:bigint"`
	ExpiredTime       int64          `json:"expired_time" gorm:"type:bigint"`
	ReceivedTime      int64          `json:"received_time" gorm:"type:bigint"`
	DeletedAt         gorm.DeletedAt `gorm:"index"`
}

func NormalizeGiftCodeReceiver(bindType string, bindValue string) (string, string, *User, error) {
	bindType = strings.TrimSpace(strings.ToLower(bindType))
	bindValue = strings.TrimSpace(bindValue)
	if bindType == "" && bindValue == "" {
		return "", "", nil, nil
	}
	if bindType == "" || bindValue == "" {
		return "", "", nil, errors.New("请同时填写绑定方式和绑定用户")
	}
	var user User
	var err error
	switch bindType {
	case GiftCodeReceiverTypeUserId:
		userId, parseErr := strconv.Atoi(bindValue)
		if parseErr != nil || userId <= 0 {
			return "", "", nil, errors.New("绑定用户 ID 无效")
		}
		err = DB.Where("id = ?", userId).First(&user).Error
		bindValue = strconv.Itoa(userId)
	case GiftCodeReceiverTypeUsername:
		err = DB.Where("username = ?", bindValue).First(&user).Error
	case GiftCodeReceiverTypeEmail:
		err = DB.Where("email = ?", bindValue).First(&user).Error
	default:
		return "", "", nil, errors.New("不支持的绑定方式")
	}
	if err != nil {
		return "", "", nil, errors.New("未找到绑定用户")
	}
	return bindType, bindValue, &user, nil
}

func CreateGiftCode(creatorUserId int, quota int, receiverUserId int, receiverBindType string, receiverBindValue string, message string, expiredTime int64) (*GiftCode, error) {
	if creatorUserId == 0 {
		return nil, errors.New("无效的生成用户")
	}
	if quota <= 0 {
		return nil, errors.New("礼品码额度必须大于 0")
	}
	giftCode := &GiftCode{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var creator User
		if err := giftCodeForUpdate(tx).First(&creator, "id = ?", creatorUserId).Error; err != nil {
			return err
		}
		if creator.Quota < quota {
			return errors.New("余额不足，无法生成礼品码")
		}
		if err := tx.Model(&creator).Update("quota", gorm.Expr("quota - ?", quota)).Error; err != nil {
			return err
		}
		giftCode = &GiftCode{
			Code:              common.GetUUID(),
			CreatorUserId:     creatorUserId,
			ReceiverUserId:    receiverUserId,
			ReceiverBindType:  receiverBindType,
			ReceiverBindValue: receiverBindValue,
			Status:            GiftCodeStatusEnabled,
			Quota:             quota,
			Message:           message,
			CreatedTime:       common.GetTimestamp(),
			ExpiredTime:       expiredTime,
		}
		return tx.Create(giftCode).Error
	})
	if err != nil {
		return nil, err
	}
	_, _ = GetUserQuota(creatorUserId, true)
	RecordLog(creatorUserId, LogTypeTopup, fmt.Sprintf("生成礼品码 %s，额度 %s", giftCode.Code, logger.LogQuota(quota)))
	return giftCode, nil
}

func giftCodeForUpdate(tx *gorm.DB) *gorm.DB {
	if tx == nil || tx.Dialector == nil || tx.Dialector.Name() == "sqlite" {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}

func ListGiftCodesByCreator(userId int, limit int) ([]*GiftCode, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var giftCodes []*GiftCode
	err := DB.Where("creator_user_id = ?", userId).Order("id desc").Limit(limit).Find(&giftCodes).Error
	if err != nil {
		return nil, err
	}
	fillGiftCodeUsernames(giftCodes)
	return giftCodes, nil
}

func GetGiftCodeByCode(code string) (*GiftCode, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("未提供礼品码")
	}
	giftCode := &GiftCode{}
	if err := DB.Where("code = ?", code).First(giftCode).Error; err != nil {
		return nil, errors.New("无效的礼品码")
	}
	fillGiftCodeUsernames([]*GiftCode{giftCode})
	return giftCode, nil
}

func ReceiveGiftCode(code string, userId int, thankMessage string) (*GiftCode, error) {
	if userId == 0 {
		return nil, errors.New("无效的接收用户")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("未提供礼品码")
	}
	giftCode := &GiftCode{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := giftCodeForUpdate(tx).Where("code = ?", code).First(giftCode).Error; err != nil {
			return errors.New("无效的礼品码")
		}
		if giftCode.Status != GiftCodeStatusEnabled {
			return errors.New("该礼品码已不可接收")
		}
		if giftCode.ExpiredTime != 0 && giftCode.ExpiredTime < common.GetTimestamp() {
			return errors.New("该礼品码已过期")
		}
		if giftCode.CreatorUserId == userId {
			return errors.New("不能接收自己生成的礼品码")
		}
		if giftCode.ReceiverUserId != 0 && giftCode.ReceiverUserId != userId {
			return errors.New("该礼品码已绑定其他用户")
		}
		result := tx.Model(&User{}).Where("id = ?", userId).Update("quota", gorm.Expr("quota + ?", giftCode.Quota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("接收用户不存在")
		}
		now := common.GetTimestamp()
		giftCode.Status = GiftCodeStatusUsed
		giftCode.ReceivedUserId = userId
		giftCode.ReceivedTime = now
		giftCode.ThankMessage = thankMessage
		return tx.Save(giftCode).Error
	})
	if err != nil {
		return nil, err
	}
	_, _ = GetUserQuota(userId, true)
	fillGiftCodeUsernames([]*GiftCode{giftCode})
	RecordLog(userId, LogTypeTopup, fmt.Sprintf("接收礼品码 %s，获得额度 %s", giftCode.Code, logger.LogQuota(giftCode.Quota)))
	RecordLog(giftCode.CreatorUserId, LogTypeSystem, fmt.Sprintf("礼品码 %s 已被 %s 接收，回信：%s", giftCode.Code, giftCode.ReceivedUsername, giftCode.ThankMessage))
	return giftCode, nil
}

func fillGiftCodeUsernames(giftCodes []*GiftCode) {
	if len(giftCodes) == 0 {
		return
	}
	userIds := make([]int, 0, len(giftCodes)*2)
	seen := map[int]bool{}
	for _, giftCode := range giftCodes {
		if giftCode.CreatorUserId != 0 && !seen[giftCode.CreatorUserId] {
			userIds = append(userIds, giftCode.CreatorUserId)
			seen[giftCode.CreatorUserId] = true
		}
		if giftCode.ReceivedUserId != 0 && !seen[giftCode.ReceivedUserId] {
			userIds = append(userIds, giftCode.ReceivedUserId)
			seen[giftCode.ReceivedUserId] = true
		}
	}
	if len(userIds) == 0 {
		return
	}
	var users []User
	if err := DB.Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return
	}
	names := map[int]string{}
	for _, user := range users {
		names[user.Id] = user.Username
	}
	for _, giftCode := range giftCodes {
		giftCode.CreatorUsername = names[giftCode.CreatorUserId]
		giftCode.ReceivedUsername = names[giftCode.ReceivedUserId]
	}
}
