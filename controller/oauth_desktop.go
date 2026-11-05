package controller

import (
	crand "crypto/rand"
	"errors"
	"math/big"
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

	// 确认码由桌面端显示、由用户在浏览器页面里输入。字符集去掉易混字符
	// （I/L/1、O/0），长度 8 位约 40 bit，配合下面的错误次数上限使用。
	desktopOAuthConfirmCodeLength      = 8
	desktopOAuthConfirmCodeAlphabet    = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	desktopOAuthConfirmMaxWrongAttempt = 5
	desktopOAuthConfirmCookieName      = "desktop_oauth_confirm"
	desktopOAuthConfirmPath            = "/api/oauth/desktop/confirm"

	// 桌面端 OAuth 失败提示只写固定文案：回调的 error 查询参数与上游错误文本
	// 都可能是攻击者可控的，原样写进请求记录会让桌面端 poll 与确认页反射出
	// 攻击者选定的内容（文案反射，可用于社工）。原始值只写服务端日志。
	desktopOAuthProviderErrorMessage = "第三方授权未能完成，请在桌面应用里重新登录"
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

	// ConfirmCode 在发起时生成，桌面端把它显示给用户；用户在网关的确认页面
	// 里输入它，handoff 才完成。ConfirmBrowserToken 只下发给打开过确认页的
	// 浏览器（HttpOnly Cookie），提交必须带上它——这样"知道 state 与确认码"
	// 不等于"能完成 handoff"。PendingUserID 是回调解析出的目标账号，
	// ConfirmationRequired 为真且 CompletedAt 为零值时，handoff 尚未完成。
	// bind 模式把真正的解绑/绑定动作也推迟到确认后执行，因此还要记住待绑定
	// 的第三方身份（ProviderSlug 已在记录上）。
	ConfirmCode           string
	ConfirmationRequired  bool
	PendingUserID         int
	PendingProviderUserID string
	ConfirmBrowserToken   string
	ConfirmationAttempts  int
}

// generateDesktopOAuthConfirmCode 用 crypto/rand 取确认码。随机源失败是系统级
// 故障，返回空串会让 handoff 变成"无码可确认"，宁可 panic 也不降级。
func generateDesktopOAuthConfirmCode() string {
	code := make([]byte, desktopOAuthConfirmCodeLength)
	max := big.NewInt(int64(len(desktopOAuthConfirmCodeAlphabet)))
	for i := range code {
		n, err := crand.Int(crand.Reader, max)
		if err != nil {
			panic("crypto/rand unavailable: " + err.Error())
		}
		code[i] = desktopOAuthConfirmCodeAlphabet[n.Int64()]
	}
	return string(code)
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
			// 确认码由桌面端显示；用户在系统浏览器的确认页面里输入它，
			// handoff 才会完成。没有它，受害者浏览器完成 IdP 授权后，
			// 持有 handoff_token 的一方仍无法领取会话。
			"confirm_code": request.ConfirmCode,
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
				// 回调已解析出账号、但浏览器还没确认时给桌面端一个提示，
				// 让它能告诉用户"请在浏览器里输入确认码"。旧客户端只读
				// status，仍按 pending 继续轮询。
				"awaiting_confirmation": request.ConfirmationRequired,
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
	issued, _ := setupLoginWithResult(user, c)
	if !issued {
		// 未签发分两种：2FA 挑战响应已由门控写入本 poll 响应（handoff 已消费，
		// 桌面端拿到 require_2fa 后在本窗口 /otp 完成验证即可签发会话）；会话保存
		// 失败也已由 setupLoginWithResult 写出错误响应。这里只需停止本次 poll。
		return
	}
}

func createDesktopOAuthRequest(providerSlug string, mode string, bindUserID int, affCode string) (*desktopOAuthRequest, error) {
	now := time.Now()
	request := &desktopOAuthRequest{
		ProviderSlug: providerSlug,
		Mode:         mode,
		State:        common.GetRandomString(24),
		HandoffToken: common.GetRandomString(40),
		ConfirmCode:  generateDesktopOAuthConfirmCode(),
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

// desktopOAuthFailureMessage 把回调拿到的错误转换成可以安全存入请求记录的
// 文案。桌面端 poll 与确认页会原样展示 ErrorMessage，因此这里只取 i18n 文案
// 或固定兜底：OAuthError.Error()/RawError 与 AccessDeniedError.Message 都可能
// 带上游或调用方可控的文本，不能原样反射给用户；原始错误仍由调用方交给
// handleOAuthError 走服务端错误响应与日志。
func desktopOAuthFailureMessage(c *gin.Context, err error) string {
	var oauthErr *oauth.OAuthError
	if errors.As(err, &oauthErr) {
		if oauthErr.Params != nil {
			return i18n.T(c, oauthErr.MsgKey, oauthErr.Params)
		}
		return i18n.T(c, oauthErr.MsgKey)
	}
	var trustLevelErr *oauth.TrustLevelError
	if errors.As(err, &trustLevelErr) {
		return i18n.T(c, i18n.MsgOAuthTrustLevelLow)
	}
	return desktopOAuthProviderErrorMessage
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
