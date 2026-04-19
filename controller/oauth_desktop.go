package controller

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	desktopOAuthModeLogin                = "login"
	desktopOAuthModeBind                 = "bind"
	desktopOAuthTTL                      = 10 * time.Minute
	desktopOAuthPollRateLimitAppliedKey  = "desktop_oauth_poll_rate_limited"
	desktopOAuthPollRateLimitFallbackKey = "DOP-FALLBACK:"
	desktopOAuthHandoffTokenMinLength    = 1
	desktopOAuthHandoffTokenMaxLength    = 128
)

var (
	desktopOAuthPollFallbackRateLimiter     common.InMemoryRateLimiter
	desktopOAuthPollFallbackRateLimiterOnce sync.Once
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
		sessionUserID, ok := id.(int)
		if !ok || sessionUserID <= 0 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "invalid current user session",
			})
			return
		}
		bindUserID = sessionUserID
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
	if !ensureDesktopOAuthPollRateLimit(c, handoffToken) {
		return
	}

	request, found, err := getDesktopOAuthRequestByHandoff(handoffToken)
	if err != nil {
		if isDesktopOAuthStoreUnavailableError(err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"message": "桌面端 OAuth 状态存储暂不可用，请稍后重试",
			})
			return
		}
		common.ApiError(c, err)
		return
	}
	if !found {
		common.ApiErrorMsg(c, "桌面端 OAuth 登录请求不存在或已过期")
		return
	}
	if request.ErrorMessage != "" {
		if _, _, consumeErr := consumeDesktopOAuthRequest(handoffToken); consumeErr != nil {
			if isDesktopOAuthStoreUnavailableError(consumeErr) {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"success": false,
					"message": "桌面端 OAuth 状态存储暂不可用，请稍后重试",
				})
				return
			}
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
		if isDesktopOAuthStoreUnavailableError(err) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"message": "桌面端 OAuth 状态存储暂不可用，请稍后重试",
			})
			return
		}
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
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorMsg(c, i18n.T(c, i18n.MsgOAuthUserBanned))
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

func ensureDesktopOAuthPollRateLimit(c *gin.Context, handoffToken string) bool {
	if _, found := c.Get(desktopOAuthPollRateLimitAppliedKey); found {
		return true
	}
	if !common.DesktopOAuthPollRateLimitEnable {
		return true
	}
	desktopOAuthPollFallbackRateLimiterOnce.Do(func() {
		desktopOAuthPollFallbackRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	})
	if desktopOAuthPollFallbackRateLimiter.Request(
		buildDesktopOAuthPollFallbackRateLimitKey(handoffToken, c.ClientIP()),
		common.DesktopOAuthPollRateLimitNum,
		common.DesktopOAuthPollRateLimitDuration,
	) {
		return true
	}
	c.JSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"message": "请求过于频繁，请稍后再试",
	})
	return false
}

func buildDesktopOAuthPollFallbackRateLimitKey(handoffToken string, clientIP string) string {
	normalizedToken := sanitizeDesktopOAuthHandoffToken(handoffToken)
	if normalizedToken != "" {
		return desktopOAuthPollRateLimitFallbackKey + normalizedToken
	}
	return desktopOAuthPollRateLimitFallbackKey + "ip:" + clientIP
}

func sanitizeDesktopOAuthHandoffToken(handoffToken string) string {
	normalized := strings.TrimSpace(handoffToken)
	if normalized == "" {
		return ""
	}
	if len(normalized) < desktopOAuthHandoffTokenMinLength ||
		len(normalized) > desktopOAuthHandoffTokenMaxLength {
		return ""
	}
	for _, char := range normalized {
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= '0' && char <= '9' {
			continue
		}
		if char == '-' || char == '_' {
			continue
		}
		return ""
	}
	return normalized
}
