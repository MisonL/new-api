package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const userHeaderTemplateListQueryTimeout = 300 * time.Millisecond

type userHeaderTemplateRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func ListUserHeaderTemplates(c *gin.Context) {
	userID := c.GetInt("id")
	ctx, cancel := context.WithTimeout(c.Request.Context(), userHeaderTemplateListQueryTimeout)
	defer cancel()

	templates, err := model.ListUserHeaderTemplatesByUserID(ctx, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	items := make([]gin.H, 0, len(templates))
	for i := range templates {
		items = append(items, buildUserHeaderTemplateResponse(&templates[i]))
	}

	common.ApiSuccess(c, items)
}

func CreateUserHeaderTemplate(c *gin.Context) {
	record, err := bindUserHeaderTemplateRequest(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	record.UserId = c.GetInt("id")
	record.CreatedAt = common.GetTimestamp()
	record.UpdatedAt = record.CreatedAt

	if err := model.DB.Create(record).Error; err != nil {
		if isDuplicateConstraintError(err) {
			common.ApiErrorMsg(c, "模板名称已存在")
			return
		}
		common.ApiError(c, err)
		return
	}

	model.RecordLog(record.UserId, model.LogTypeManage, fmt.Sprintf("创建请求头模板：%s", record.Name))
	common.ApiSuccess(c, buildUserHeaderTemplateResponse(record))
}

func UpdateUserHeaderTemplate(c *gin.Context) {
	templateID, ok := parseHeaderTemplateID(c)
	if !ok {
		return
	}

	userID := c.GetInt("id")
	record := model.UserHeaderTemplate{}
	if err := model.DB.Where("id = ? AND user_id = ?", templateID, userID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "模板不存在")
			return
		}
		common.ApiError(c, err)
		return
	}

	updated, err := bindUserHeaderTemplateRequest(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	record.Name = updated.Name
	record.Content = updated.Content
	record.UpdatedAt = common.GetTimestamp()

	if err := model.DB.Save(&record).Error; err != nil {
		if isDuplicateConstraintError(err) {
			common.ApiErrorMsg(c, "模板名称已存在")
			return
		}
		common.ApiError(c, err)
		return
	}

	model.RecordLog(userID, model.LogTypeManage, fmt.Sprintf("更新请求头模板：%s", record.Name))
	common.ApiSuccess(c, buildUserHeaderTemplateResponse(&record))
}

func DeleteUserHeaderTemplate(c *gin.Context) {
	templateID, ok := parseHeaderTemplateID(c)
	if !ok {
		return
	}

	userID := c.GetInt("id")
	result := model.DB.Where("id = ? AND user_id = ?", templateID, userID).Delete(&model.UserHeaderTemplate{})
	if result.Error != nil {
		common.ApiError(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		common.ApiErrorMsg(c, "模板不存在")
		return
	}

	model.RecordLog(userID, model.LogTypeManage, fmt.Sprintf("删除请求头模板：ID=%d", templateID))
	common.ApiSuccess(c, gin.H{})
}

func bindUserHeaderTemplateRequest(c *gin.Context) (*model.UserHeaderTemplate, error) {
	var req userHeaderTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, errors.New("参数错误")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("模板名称不能为空")
	}
	if len(name) > 128 {
		return nil, errors.New("模板名称长度不能超过128个字符")
	}

	content, err := normalizeRequiredHeaderTemplateForStorage(req.Content)
	if err != nil {
		return nil, err
	}

	return &model.UserHeaderTemplate{
		Name:    name,
		Content: content,
	}, nil
}

func normalizeRequiredHeaderTemplateForStorage(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("模板内容不能为空")
	}

	content, err := normalizeHeaderTemplateForStorage(&trimmed)
	if err != nil {
		return "", err
	}
	if content == "" {
		return "", errors.New("模板内容不能为空")
	}

	return content, nil
}

func parseHeaderTemplateID(c *gin.Context) (int, bool) {
	templateID, err := strconv.Atoi(c.Param("id"))
	if err != nil || templateID <= 0 {
		common.ApiErrorMsg(c, "模板ID不合法")
		return 0, false
	}

	return templateID, true
}

func buildUserHeaderTemplateResponse(record *model.UserHeaderTemplate) gin.H {
	return gin.H{
		"id":         record.Id,
		"name":       record.Name,
		"content":    record.Content,
		"created_at": record.CreatedAt,
		"updated_at": record.UpdatedAt,
	}
}

func isDuplicateConstraintError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "duplicate") || strings.Contains(lower, "unique")
}
