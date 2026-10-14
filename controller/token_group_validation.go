package controller

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// validateTokenGroup rejects a token group the owning user could never use.
//
// TokenAuth already refuses such a token at request time, but only after it has
// been stored: the API accepted the group, it was written to the database and
// listed back to the user, and then every request failed with
// "无权访问 <group> 分组". Validating on write turns that into one clear
// rejection at the point the value is entered.
//
// An empty group is valid and means "use the user's own group". The auto group
// is valid because TokenAuth normalises it to "auto" and resolves it per
// request.
func validateTokenGroup(c *gin.Context, group string) error {
	trimmed := strings.TrimSpace(group)
	if trimmed == "" {
		return nil
	}
	if common.IsAutoGroup(trimmed) {
		return nil
	}
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if _, ok := service.GetUserUsableGroups(userGroup)[trimmed]; !ok {
		return fmt.Errorf("无权访问 %s 分组", trimmed)
	}
	if !ratio_setting.ContainsGroupRatio(trimmed) {
		return fmt.Errorf("分组 %s 已被弃用", trimmed)
	}
	return nil
}
