package controller

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type tagHeaderPolicyRequest struct {
	Tag                     string                 `json:"tag"`
	HeaderOverride          *string                `json:"header_override"`
	HeaderPolicyMode        string                 `json:"header_policy_mode"`
	OverrideHeaderUserAgent bool                   `json:"override_header_user_agent"`
	UserAgentStrategy       *dto.UserAgentStrategy `json:"ua_strategy"`
}

func GetTagHeaderPolicy(c *gin.Context) {
	tag := strings.TrimSpace(c.Query("tag"))
	if tag == "" {
		common.ApiErrorMsg(c, "tag不能为空")
		return
	}

	var record model.TagRequestHeaderPolicy
	err := model.DB.Where("tag = ?", tag).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiSuccess(c, buildTagHeaderPolicyResponse(tag, nil, nil))
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	strategy, err := decodeStoredUserAgentStrategy(record.UserAgentStrategyJSON)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	common.ApiSuccess(c, buildTagHeaderPolicyResponse(tag, &record, strategy))
}

func UpsertTagHeaderPolicy(c *gin.Context) {
	var req tagHeaderPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	tag := strings.TrimSpace(req.Tag)
	if tag == "" {
		common.ApiErrorMsg(c, "tag不能为空")
		return
	}

	headerOverride, err := normalizeHeaderTemplateForStorage(req.HeaderOverride)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	mode, err := normalizeHeaderPolicyMode(req.HeaderPolicyMode)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	strategy, err := normalizeUserAgentStrategyForStorage(req.UserAgentStrategy)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	strategyJSON := ""
	if strategy != nil {
		raw, err := common.Marshal(strategy)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		strategyJSON = string(raw)
	}

	now := common.GetTimestamp()
	record := model.TagRequestHeaderPolicy{}
	err = model.DB.Where("tag = ?", tag).First(&record).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		record = model.TagRequestHeaderPolicy{
			Tag:                     tag,
			HeaderOverride:          headerOverride,
			HeaderPolicyMode:        mode,
			OverrideHeaderUserAgent: req.OverrideHeaderUserAgent,
			UserAgentStrategyJSON:   strategyJSON,
			CreatedAt:               now,
			UpdatedAt:               now,
		}
		if err := model.DB.Create(&record).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	case err != nil:
		common.ApiError(c, err)
		return
	default:
		record.HeaderOverride = headerOverride
		record.HeaderPolicyMode = mode
		record.OverrideHeaderUserAgent = req.OverrideHeaderUserAgent
		record.UserAgentStrategyJSON = strategyJSON
		record.UpdatedAt = now
		if err := model.DB.Save(&record).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	}

	if userID := c.GetInt("id"); userID > 0 {
		model.RecordLog(userID, model.LogTypeManage, fmt.Sprintf("更新标签请求头策略：tag=%s", tag))
	}
	common.ApiSuccess(c, buildTagHeaderPolicyResponse(tag, &record, strategy))
}

func DeleteTagHeaderPolicy(c *gin.Context) {
	tag := strings.TrimSpace(c.Query("tag"))
	if tag == "" {
		common.ApiErrorMsg(c, "tag不能为空")
		return
	}

	if err := model.DB.Where("tag = ?", tag).Delete(&model.TagRequestHeaderPolicy{}).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	if userID := c.GetInt("id"); userID > 0 {
		model.RecordLog(userID, model.LogTypeManage, fmt.Sprintf("删除标签请求头策略：tag=%s", tag))
	}
	common.ApiSuccess(c, gin.H{})
}

func normalizeHeaderTemplateForStorage(raw *string) (string, error) {
	if raw == nil {
		return "", nil
	}

	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return "", nil
	}

	headers, err := service.ValidateHeaderTemplate(trimmed)
	if err != nil {
		return "", err
	}
	if headers == nil {
		return "", nil
	}

	content, err := common.Marshal(headers)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func normalizeHeaderPolicyMode(raw string) (string, error) {
	mode := strings.TrimSpace(raw)
	if mode == "" {
		return string(dto.HeaderPolicyModeSystemDefault), nil
	}

	switch dto.HeaderPolicyMode(mode) {
	case dto.HeaderPolicyModeSystemDefault, dto.HeaderPolicyModePreferChannel, dto.HeaderPolicyModePreferTag, dto.HeaderPolicyModeMerge:
		return mode, nil
	default:
		return "", errors.New("请求头优先级模式不合法")
	}
}

func normalizeUserAgentStrategyForStorage(strategy *dto.UserAgentStrategy) (*dto.UserAgentStrategy, error) {
	if strategy == nil {
		return nil, nil
	}

	result, err := service.ResolveUserAgentStrategy(strategy)
	if err != nil {
		return nil, err
	}
	if strategy.Enabled {
		return result.Strategy, nil
	}

	normalized := &dto.UserAgentStrategy{Enabled: false}
	mode := strings.TrimSpace(strategy.Mode)
	if mode != "" {
		normalized.Mode = mode
	}
	userAgents := service.MergeUserAgents(strategy.UserAgents, nil)
	if len(userAgents) > 0 {
		normalized.UserAgents = userAgents
	}

	return normalized, nil
}

func decodeStoredUserAgentStrategy(raw string) (*dto.UserAgentStrategy, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	var strategy dto.UserAgentStrategy
	if err := common.UnmarshalJsonStr(trimmed, &strategy); err != nil {
		return nil, errors.New("已存储的UA策略格式不合法")
	}

	normalized, err := normalizeUserAgentStrategyForStorage(&strategy)
	if err != nil {
		return nil, err
	}

	return normalized, nil
}

func buildTagHeaderPolicyResponse(tag string, record *model.TagRequestHeaderPolicy, strategy *dto.UserAgentStrategy) gin.H {
	if record == nil {
		return gin.H{
			"exists":                     false,
			"tag":                        tag,
			"header_override":            "",
			"header_policy_mode":         string(dto.HeaderPolicyModeSystemDefault),
			"override_header_user_agent": false,
			"ua_strategy":                nil,
		}
	}

	return gin.H{
		"exists":                     true,
		"tag":                        tag,
		"header_override":            record.HeaderOverride,
		"header_policy_mode":         record.HeaderPolicyMode,
		"override_header_user_agent": record.OverrideHeaderUserAgent,
		"ua_strategy":                strategy,
		"created_at":                 record.CreatedAt,
		"updated_at":                 record.UpdatedAt,
	}
}
