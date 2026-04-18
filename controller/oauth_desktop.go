package controller

import (
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	desktopOAuthModeLogin = "login"
	desktopOAuthModeBind  = "bind"
	desktopOAuthTTL       = 10 * time.Minute
)

type desktopOAuthRequest struct {
	ProviderSlug string
	Mode         string
	State        string
	HandoffToken string
	BindUserID   int
	AffCode      string
	CreatedAt    time.Time
	CompletedAt  time.Time
	ResultUserID int
	ErrorMessage string
}

func StartDesktopOAuth(c *gin.Context) {
	providerSlug := strings.TrimSpace(c.Query("provider"))
	if providerSlug == "" {
		common.ApiErrorMsg(c, "缺少 OAuth 提供商")
		return
	}

	provider := oauth.GetProvider(providerSlug)
	if provider == nil {
		common.ApiErrorMsg(c, "Unknown OAuth provider")
		return
	}
	if !provider.IsEnabled() {
		common.ApiErrorMsg(c, "OAuth provider is not enabled")
		return
	}

	mode := strings.TrimSpace(c.Query("mode"))
	if mode == "" {
		mode = desktopOAuthModeLogin
	}
	if mode != desktopOAuthModeLogin && mode != desktopOAuthModeBind {
		common.ApiErrorMsg(c, "无效的桌面端 OAuth 模式")
		return
	}

	bindUserID := 0
	if mode == desktopOAuthModeBind {
		session := sessions.Default(c)
		id := session.Get("id")
		if id == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "missing current user session",
			})
			return
		}
		bindUserID = id.(int)
	}

	request, err := createDesktopOAuthRequest(providerSlug, mode, bindUserID, c.Query("aff"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"state":         request.State,
			"handoff_token": request.HandoffToken,
			"mode":          request.Mode,
		},
	})
}

func PollDesktopOAuth(c *gin.Context) {
	handoffToken := strings.TrimSpace(c.Query("handoff_token"))
	if handoffToken == "" {
		common.ApiErrorMsg(c, "缺少桌面端 OAuth handoff token")
		return
	}

	request, found, err := getDesktopOAuthRequestByHandoff(handoffToken)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !found {
		common.ApiErrorMsg(c, "桌面端 OAuth 登录请求不存在或已过期")
		return
	}
	if request.ErrorMessage != "" {
		if _, _, consumeErr := consumeDesktopOAuthRequest(handoffToken); consumeErr != nil {
			common.ApiError(c, consumeErr)
			return
		}
		common.ApiErrorMsg(c, request.ErrorMessage)
		return
	}
	if request.CompletedAt.IsZero() {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"status": "pending",
			},
		})
		return
	}

	if _, _, err := consumeDesktopOAuthRequest(handoffToken); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.Mode == desktopOAuthModeBind {
		common.ApiSuccess(c, gin.H{
			"status": "completed",
			"action": "bind",
		})
		return
	}

	user := &model.User{Id: request.ResultUserID}
	if err := user.FillUserById(); err != nil {
		common.ApiError(c, err)
		return
	}
	setupLoginWithResult(user, c)
}

func createDesktopOAuthRequest(providerSlug string, mode string, bindUserID int, affCode string) (*desktopOAuthRequest, error) {
	now := time.Now()
	request := &desktopOAuthRequest{
		ProviderSlug: providerSlug,
		Mode:         mode,
		State:        common.GetRandomString(24),
		HandoffToken: common.GetRandomString(40),
		BindUserID:   bindUserID,
		AffCode:      strings.TrimSpace(affCode),
		CreatedAt:    now,
	}

	if err := currentDesktopOAuthStore().Create(request); err != nil {
		return nil, err
	}
	return request, nil
}

func getDesktopOAuthRequestByState(state string) (*desktopOAuthRequest, bool, error) {
	return currentDesktopOAuthStore().GetByState(state)
}

func getDesktopOAuthRequestByHandoff(handoffToken string) (*desktopOAuthRequest, bool, error) {
	return currentDesktopOAuthStore().GetByHandoff(handoffToken)
}

func completeDesktopOAuthRequest(state string, resultUserID int) error {
	return currentDesktopOAuthStore().Complete(state, resultUserID)
}

func failDesktopOAuthRequest(state string, message string) error {
	return currentDesktopOAuthStore().Fail(state, message)
}

func consumeDesktopOAuthRequest(handoffToken string) (*desktopOAuthRequest, bool, error) {
	return currentDesktopOAuthStore().Consume(handoffToken)
}
