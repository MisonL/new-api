package controller

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 桌面端 bind 流程：用户在桌面应用里发起绑定 -> 浏览器回调只登记"待确认"
// -> 跳到确认页 -> 输入确认码。确认页在 bind 成功后必须尝试关闭自己，否则
// 用户在浏览器里留下一个永远停在"授权已完成"的标签页，桌面端也拿不到结果。
// 登录流程不关窗：同一浏览器可能还要继续用。
func TestDesktopOAuthConfirmBindSuccessClosesWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	registerDesktopConfirmProvider(t, "desk-bind-close")

	router := gin.New()
	router.Use(sessions.Sessions("desktop-oauth-test", cookie.NewStore([]byte("desktop-oauth-close-test-secret"))))
	router.GET("/api/oauth/desktop/start", StartDesktopOAuth)
	router.GET("/api/oauth/desktop/confirm", DesktopOAuthConfirmPage)
	router.POST("/api/oauth/desktop/confirm", DesktopOAuthConfirmSubmit)
	router.GET("/api/oauth/:provider", HandleOAuth)
	router.GET("/test/login-as/:id", func(c *gin.Context) {
		var user model.User
		if err := model.DB.First(&user, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
			return
		}
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		_ = session.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	server := httptest.NewServer(router)
	defer server.Close()

	client := newTestHTTPClient(t)
	user := createUserForBindTest(t, "desk-bind-close-user")
	loginReq, err := http.NewRequest(http.MethodGet, server.URL+"/test/login-as/"+strconv.Itoa(user.Id), nil)
	require.NoError(t, err)
	loginResp, err := client.Do(loginReq)
	require.NoError(t, err)
	_ = loginResp.Body.Close()

	startResp, err := client.Get(server.URL + "/api/oauth/desktop/start?provider=desk-bind-close&mode=bind")
	require.NoError(t, err)
	startBody, err := io.ReadAll(startResp.Body)
	require.NoError(t, err)
	_ = startResp.Body.Close()
	var startPayload struct {
		Success bool `json:"success"`
		Data    struct {
			State       string `json:"state"`
			ConfirmCode string `json:"confirm_code"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(startBody, &startPayload))
	require.True(t, startPayload.Success, string(startBody))
	require.NotEmpty(t, startPayload.Data.State)

	// 浏览器完成第三方授权：回调只登记待确认，并下发浏览器令牌。
	callbackURL := server.URL + "/api/oauth/desk-bind-close?code=x&state=" + url.QueryEscape(startPayload.Data.State)
	callbackResp, err := client.Get(callbackURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, callbackResp.StatusCode)
	_, _ = io.Copy(io.Discard, callbackResp.Body)
	_ = callbackResp.Body.Close()

	// 确认页（GET）必须能看到账号与确认表单，而不是"已完成"。
	confirmPageResp, err := client.Get(server.URL + "/api/oauth/desktop/confirm?state=" + url.QueryEscape(startPayload.Data.State))
	require.NoError(t, err)
	pageBody, err := io.ReadAll(confirmPageResp.Body)
	require.NoError(t, err)
	_ = confirmPageResp.Body.Close()
	require.NotContains(t, string(pageBody), "window.close()",
		"确认页在用户输入确认码前不应关闭窗口")

	// 提交确认码：bind 模式在确认之后才真正写入绑定。
	form := url.Values{}
	form.Set("state", startPayload.Data.State)
	form.Set("code", startPayload.Data.ConfirmCode)
	submitResp, err := client.Post(server.URL+"/api/oauth/desktop/confirm", "application/x-www-form-urlencoded", bytes.NewBufferString(form.Encode()))
	require.NoError(t, err)
	submitBody, err := io.ReadAll(submitResp.Body)
	require.NoError(t, err)
	_ = submitResp.Body.Close()
	require.Equal(t, http.StatusOK, submitResp.StatusCode)
	require.Contains(t, string(submitBody), "window.close()",
		"bind 成功的确认页必须尝试关闭浏览器窗口")
}

// 反向对照：桌面端 login 成功后不应关闭窗口。
func TestDesktopOAuthConfirmLoginSuccessDoesNotCloseWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)
	registerDesktopConfirmProvider(t, "desk-login-noclose")

	router := newDesktopConfirmTestRouter()
	server := httptest.NewServer(router)
	defer server.Close()

	client := newTestHTTPClient(t)
	state, _, confirmCode := startDesktopFlow(t, router, "desk-login-noclose")

	callbackResp, err := client.Get(server.URL + "/api/oauth/desk-login-noclose?code=x&state=" + url.QueryEscape(state))
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, callbackResp.Body)
	_ = callbackResp.Body.Close()

	form := url.Values{}
	form.Set("state", state)
	form.Set("code", confirmCode)
	submitResp, err := client.Post(server.URL+"/api/oauth/desktop/confirm", "application/x-www-form-urlencoded", bytes.NewBufferString(form.Encode()))
	require.NoError(t, err)
	submitBody, err := io.ReadAll(submitResp.Body)
	require.NoError(t, err)
	_ = submitResp.Body.Close()
	require.NotContains(t, string(submitBody), "window.close()",
		"桌面端 login 成功后不应关闭窗口")
}
