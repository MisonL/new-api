package controller

import (
	"crypto/subtle"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-gonic/gin"
)

// 桌面端登录/绑定的回调跑在系统浏览器里，会话却由桌面应用轮询领取。若回调
// 解析出账号就立刻完成 handoff，任何把授权链接送到别人浏览器的人，都能让
// 那台浏览器把账号交给发起轮询的自己。因此回调只把解析结果记成"待确认"，
// 必须在那台浏览器里输入桌面应用显示的确认码才完成。
//
// 两道条件各自挡住一半攻击面：
//
//   - 确认码：只有正在桌面端前的用户知道，证明"授权后有有意识的确认动作"。
//     攻击者用自己的客户端提交 state + 确认码时，浏览器令牌对不上。
//   - 浏览器令牌：回调响应直接下发给完成授权的浏览器的 HttpOnly Cookie，
//     记录里保存同一份。确认页不重新签发令牌，攻击者无法用自己的 GET
//     覆盖它——只存在受害者浏览器里的值决定了只有那台浏览器能提交。
//
// 提交还要求同源（Origin / Sec-Fetch-Site），挡住跨站表单代提交。

const desktopOAuthConfirmTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}}</title>
<style>
:root { color-scheme: light dark; }
body { margin: 0; padding: 24px 16px 48px; font: 15px/1.6 -apple-system, "Segoe UI", "Helvetica Neue", Arial, sans-serif; background: #f6f7f9; color: #1f2329; }
.card { max-width: 460px; margin: 40px auto 0; padding: 28px; background: #fff; border: 1px solid #e2e5ea; border-radius: 10px; }
h1 { margin: 0 0 12px; font-size: 20px; }
p { margin: 0 0 14px; }
.account { padding: 10px 12px; margin: 0 0 16px; background: #f2f4f7; border-radius: 6px; font-weight: 600; word-break: break-all; }
label { display: block; margin-bottom: 6px; font-size: 14px; color: #4a5160; }
input { width: 100%; box-sizing: border-box; padding: 10px 12px; font-size: 18px; letter-spacing: 3px; text-transform: uppercase; border: 1px solid #c9ced6; border-radius: 6px; background: #fff; color: #1f2329; }
button { margin-top: 16px; width: 100%; padding: 11px 14px; font-size: 15px; font-weight: 600; border: 0; border-radius: 6px; background: #1f6feb; color: #fff; cursor: pointer; }
.notice { margin-top: 16px; padding: 12px; font-size: 13px; color: #4a5160; background: #fff8e6; border: 1px solid #f0d48a; border-radius: 6px; }
.error { margin-bottom: 16px; padding: 12px; font-size: 14px; color: #a8071a; background: #fff1f0; border: 1px solid #ffccc7; border-radius: 6px; }
@media (prefers-color-scheme: dark) {
body { background: #17181a; color: #e6e8eb; }
.card { background: #22242a; border-color: #33363d; }
.account { background: #2b2e35; }
p, label, .notice, .error { color: #d5d8dd; }
input { background: #1b1d21; color: #e6e8eb; border-color: #3a3e46; }
.notice { background: #2e2716; border-color: #5a4a22; }
.error { background: #33191a; border-color: #5c2a2c; }
}
</style>
</head>
<body>
<div class="card">
<h1>{{.Title}}</h1>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
{{if .Done}}
<p>{{.DoneMessage}}</p>
{{if .CloseAfterDone}}<script>try{window.close()}catch(_e){}</script>{{end}}
{{else}}
<p>下面这个账号即将在桌面应用中登录：</p>
<div class="account">{{.Account}}</div>
{{if .IsBind}}<p>本次操作会把该第三方账号绑定到这个账户。</p>{{end}}
<form method="post" action="{{.Action}}">
<input type="hidden" name="state" value="{{.State}}">
<label for="code">请输入桌面应用中显示的确认码</label>
<input id="code" name="code" autocomplete="one-time-code" autocapitalize="characters" spellcheck="false" maxlength="{{.CodeLength}}" required>
<button type="submit">确认登录</button>
</form>
<div class="notice">如果这不是您本人发起的登录，请关闭本页面，不要输入任何确认码。</div>
{{end}}
</div>
</body>
</html>
`

type desktopOAuthConfirmPageData struct {
	Title          string
	Account        string
	State          string
	Action         string
	Error          string
	CodeLength     int
	IsBind         bool
	Done           bool
	DoneMessage    string
	CloseAfterDone bool
}

var desktopOAuthConfirmPageTemplate = template.Must(
	template.New("desktop_oauth_confirm").Parse(desktopOAuthConfirmTemplate),
)

// stageDesktopOAuthConfirmation 把回调解析出的账号记成"待确认"，并把一次性
// 浏览器令牌写进当前响应：令牌同时保存在记录里与该浏览器的 HttpOnly Cookie
// 中，之后只有这台浏览器能提交确认。
func stageDesktopOAuthConfirmation(c *gin.Context, state string, pendingUserID int, pendingProviderUserID string) error {
	token := common.GetRandomString(32)
	if err := currentDesktopOAuthStore().AwaitConfirmation(state, pendingUserID, pendingProviderUserID, token); err != nil {
		return err
	}
	setDesktopOAuthConfirmBrowserCookie(c, token)
	return nil
}

// respondDesktopOAuthAwaitingConfirmation 把回调结果交给浏览器：顶层导航直接
// 跳到确认页，前端 XHR 则拿到带 confirm_url 的 JSON（由 SPA 负责跳转）。
func respondDesktopOAuthAwaitingConfirmation(c *gin.Context, state string, action string) {
	confirmURL := desktopOAuthConfirmPath + "?state=" + url.QueryEscape(state)
	if desktopOAuthCallbackExpectsHTML(c) {
		c.Redirect(http.StatusFound, confirmURL)
		return
	}
	common.ApiSuccess(c, gin.H{
		"desktop_handoff":  true,
		"action":           action,
		"confirm_required": true,
		"confirm_url":      confirmURL,
	})
}

func respondDesktopOAuthCallbackFailure(c *gin.Context, message string) {
	if desktopOAuthCallbackExpectsHTML(c) {
		renderDesktopOAuthConfirmPage(c, http.StatusBadRequest, desktopOAuthConfirmPageData{
			Title:       "无法完成桌面端登录",
			Error:       message,
			Done:        true,
			DoneMessage: "请关闭本页面，在桌面应用里重新发起登录。",
		})
		return
	}
	common.ApiErrorMsg(c, message)
}

func desktopOAuthCallbackExpectsHTML(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "text/html")
}

func renderDesktopOAuthConfirmPage(c *gin.Context, status int, data desktopOAuthConfirmPageData) {
	data.Action = desktopOAuthConfirmPath
	data.CodeLength = desktopOAuthConfirmCodeLength
	if data.Title == "" {
		data.Title = "确认桌面端登录"
	}
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	if err := desktopOAuthConfirmPageTemplate.Execute(c.Writer, data); err != nil {
		common.SysError("failed to render desktop oauth confirm page: " + err.Error())
	}
}

func setDesktopOAuthConfirmBrowserCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     desktopOAuthConfirmCookieName,
		Value:    token,
		Path:     desktopOAuthConfirmPath,
		MaxAge:   int(desktopOAuthTTL / time.Second),
		HttpOnly: true,
		Secure:   c.Request.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearDesktopOAuthConfirmBrowserCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     desktopOAuthConfirmCookieName,
		Value:    "",
		Path:     desktopOAuthConfirmPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.Request.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func desktopOAuthConfirmBrowserToken(c *gin.Context) string {
	cookie, err := c.Request.Cookie(desktopOAuthConfirmCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func desktopOAuthConfirmationReady(request *desktopOAuthRequest) bool {
	return request != nil &&
		request.ConfirmationRequired &&
		request.PendingUserID != 0 &&
		request.CompletedAt.IsZero()
}

// desktopOAuthConfirmRequestIsSameOrigin 拒绝跨站提交：Origin 存在时必须与
// 网关自己的来源一致；Sec-Fetch-Site 存在时必须是 same-origin。两者都缺失
// 的请求（非浏览器客户端）仍受浏览器令牌约束。
func desktopOAuthConfirmRequestIsSameOrigin(c *gin.Context) bool {
	if origin := strings.TrimSpace(c.GetHeader("Origin")); origin != "" {
		return origin == desktopOAuthRequestOrigin(c)
	}
	if site := strings.TrimSpace(c.GetHeader("Sec-Fetch-Site")); site != "" {
		return site == "same-origin" || site == "none"
	}
	return true
}

func desktopOAuthRequestOrigin(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx >= 0 {
			forwarded = forwarded[:idx]
		}
		scheme = strings.TrimSpace(forwarded)
	}
	return scheme + "://" + c.Request.Host
}

// DesktopOAuthConfirmPage 渲染确认页。令牌由回调响应下发，这里只校验当前
// 浏览器是否就是完成授权的那一台；不匹配时不展示账号信息，也不渲染表单。
func DesktopOAuthConfirmPage(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	if state == "" {
		renderDesktopOAuthConfirmPage(c, http.StatusBadRequest, desktopOAuthConfirmPageData{
			Error: "缺少确认参数，请在桌面应用里重新发起登录",
			Done:  true,
		})
		return
	}

	request, found, err := getDesktopOAuthRequestByState(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !found {
		renderDesktopOAuthConfirmPage(c, http.StatusNotFound, desktopOAuthConfirmPageData{
			Error:       "登录请求不存在或已过期",
			Done:        true,
			DoneMessage: "请关闭本页面，在桌面应用里重新发起登录。",
		})
		return
	}
	if !request.CompletedAt.IsZero() {
		renderDesktopOAuthConfirmPage(c, http.StatusOK, desktopOAuthConfirmPageData{
			Done:        true,
			DoneMessage: "该登录请求已确认完成，请返回桌面应用。",
		})
		return
	}
	if !desktopOAuthConfirmationReady(request) {
		message := request.ErrorMessage
		if message == "" {
			message = "该登录请求还不能确认，请先在浏览器里完成第三方授权。"
		}
		renderDesktopOAuthConfirmPage(c, http.StatusBadRequest, desktopOAuthConfirmPageData{
			Error:       message,
			Done:        true,
			DoneMessage: "请关闭本页面，在桌面应用里重新发起登录。",
		})
		return
	}
	if subtle.ConstantTimeCompare(
		[]byte(desktopOAuthConfirmBrowserToken(c)),
		[]byte(request.ConfirmBrowserToken),
	) != 1 {
		renderDesktopOAuthConfirmPage(c, http.StatusForbidden, desktopOAuthConfirmPageData{
			Error:       "请在完成第三方授权的同一个浏览器里继续本次确认。",
			Done:        true,
			DoneMessage: "如果这不是您本人发起的登录，请关闭本页面。",
		})
		return
	}

	renderDesktopOAuthConfirmPage(c, http.StatusOK, desktopOAuthConfirmPageData{
		State:   state,
		Account: describeDesktopOAuthPendingAccount(request.PendingUserID),
		IsBind:  request.Mode == desktopOAuthModeBind,
	})
}

// DesktopOAuthConfirmSubmit 校验"完成授权的同一台浏览器 + 桌面端确认码"，
// 两者都通过才完成 handoff；bind 模式同时执行真正的绑定动作。
func DesktopOAuthConfirmSubmit(c *gin.Context) {
	state := strings.TrimSpace(c.PostForm("state"))
	code := strings.ToUpper(strings.TrimSpace(c.PostForm("code")))
	if state == "" {
		renderDesktopOAuthConfirmPage(c, http.StatusBadRequest, desktopOAuthConfirmPageData{
			Error: "缺少确认参数，请在桌面应用里重新发起登录",
			Done:  true,
		})
		return
	}
	if !desktopOAuthConfirmRequestIsSameOrigin(c) {
		renderDesktopOAuthConfirmPage(c, http.StatusForbidden, desktopOAuthConfirmPageData{
			Error:       "拒绝跨站提交的确认请求。",
			Done:        true,
			DoneMessage: "请回到确认页面重新提交。",
		})
		return
	}

	request, found, err := getDesktopOAuthRequestByState(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !found {
		renderDesktopOAuthConfirmPage(c, http.StatusNotFound, desktopOAuthConfirmPageData{
			Error:       "登录请求不存在或已过期",
			Done:        true,
			DoneMessage: "请关闭本页面，在桌面应用里重新发起登录。",
		})
		return
	}
	if !request.CompletedAt.IsZero() {
		renderDesktopOAuthConfirmPage(c, http.StatusOK, desktopOAuthConfirmPageData{
			Done:        true,
			DoneMessage: "该登录请求已确认完成，请返回桌面应用。",
		})
		return
	}
	if !desktopOAuthConfirmationReady(request) {
		renderDesktopOAuthConfirmPage(c, http.StatusBadRequest, desktopOAuthConfirmPageData{
			Error:       "该登录请求还不能确认，请先在浏览器里完成第三方授权。",
			Done:        true,
			DoneMessage: "请关闭本页面，在桌面应用里重新发起登录。",
		})
		return
	}
	if subtle.ConstantTimeCompare(
		[]byte(desktopOAuthConfirmBrowserToken(c)),
		[]byte(request.ConfirmBrowserToken),
	) != 1 {
		renderDesktopOAuthConfirmPage(c, http.StatusForbidden, desktopOAuthConfirmPageData{
			Error:       "当前浏览器不是完成本次授权的浏览器，请在刚完成授权的浏览器里输入确认码。",
			Done:        true,
			DoneMessage: "如果这不是您本人发起的登录，请关闭本页面。",
		})
		return
	}

	if request.ConfirmCode == "" ||
		subtle.ConstantTimeCompare([]byte(code), []byte(strings.ToUpper(request.ConfirmCode))) != 1 {
		attempts, incrementErr := currentDesktopOAuthStore().IncrementConfirmationAttempts(state)
		if incrementErr != nil {
			common.ApiError(c, incrementErr)
			return
		}
		if attempts < 0 {
			renderDesktopOAuthConfirmPage(c, http.StatusNotFound, desktopOAuthConfirmPageData{
				Error:       "登录请求已过期",
				Done:        true,
				DoneMessage: "请关闭本页面，在桌面应用里重新发起登录。",
			})
			return
		}
		if attempts >= desktopOAuthConfirmMaxWrongAttempt {
			if failErr := failDesktopOAuthRequest(state, "确认码错误次数过多，请重新发起登录"); failErr != nil {
				common.ApiError(c, failErr)
				return
			}
			clearDesktopOAuthConfirmBrowserCookie(c)
			renderDesktopOAuthConfirmPage(c, http.StatusBadRequest, desktopOAuthConfirmPageData{
				Error:       "确认码错误次数过多，本次登录请求已作废。",
				Done:        true,
				DoneMessage: "请在桌面应用里重新发起登录。",
			})
			return
		}
		renderDesktopOAuthConfirmPage(c, http.StatusBadRequest, desktopOAuthConfirmPageData{
			State:   state,
			Error:   "确认码不正确，请核对桌面应用中显示的确认码。",
			Account: describeDesktopOAuthPendingAccount(request.PendingUserID),
			IsBind:  request.Mode == desktopOAuthModeBind,
		})
		return
	}

	if request.Mode == desktopOAuthModeBind {
		if bindErr := applyDesktopOAuthPendingBind(request); bindErr != nil {
			_ = failDesktopOAuthRequest(state, bindErr.Error())
			clearDesktopOAuthConfirmBrowserCookie(c)
			renderDesktopOAuthConfirmPage(c, http.StatusBadRequest, desktopOAuthConfirmPageData{
				Error:       bindErr.Error(),
				Done:        true,
				DoneMessage: "请关闭本页面，在桌面应用里重新发起操作。",
			})
			return
		}
	}

	if err := completeDesktopOAuthRequest(state, request.PendingUserID); err != nil {
		common.ApiError(c, err)
		return
	}
	// 完成后清掉浏览器令牌：同一次 handoff 只能被确认一次。
	clearDesktopOAuthConfirmBrowserCookie(c)
	renderDesktopOAuthConfirmPage(c, http.StatusOK, desktopOAuthConfirmPageData{
		Title:          "确认完成",
		Done:           true,
		DoneMessage:    "授权已完成，请返回桌面应用继续。",
		CloseAfterDone: request.Mode == desktopOAuthModeBind,
	})
}

// applyDesktopOAuthPendingBind 在确认之后才真正写入绑定关系。回调本身只做
// 只读校验，否则把授权链接送进受害者浏览器的人，无需确认就能把受害者的
// 第三方身份挂到自己账户上。
func applyDesktopOAuthPendingBind(request *desktopOAuthRequest) error {
	provider := oauth.GetProvider(request.ProviderSlug)
	if provider == nil {
		return errors.New("OAuth 提供商不存在或已禁用")
	}
	user := &model.User{Id: request.PendingUserID}
	if err := user.FillUserById(); err != nil {
		return err
	}
	oauthUser := &oauth.OAuthUser{ProviderUserID: request.PendingProviderUserID}
	if err := ensureProviderBindingAvailableForUser(user, provider, oauthUser); err != nil {
		return err
	}
	return bindOAuthIdentityToUser(user, provider, request.PendingProviderUserID)
}

func describeDesktopOAuthPendingAccount(userID int) string {
	user := &model.User{Id: userID}
	if err := user.FillUserById(); err != nil {
		return "账户 #" + strconv.Itoa(userID)
	}
	name := strings.TrimSpace(user.Username)
	if name == "" {
		name = "用户"
	}
	if display := strings.TrimSpace(user.DisplayName); display != "" && display != name {
		return name + "（" + display + "）"
	}
	return name
}
