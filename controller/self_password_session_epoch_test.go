package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 改密码要吊销其它设备上的会话，但操作者本人不应该被顺带踢下线：自助改密
// 成功后必须把递增后的世代写回当前会话。否则用户改完密码的下一个请求就
// 401，看起来像改密失败。
func TestUpdateSelfKeepsCurrentSessionAfterPasswordChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupCustomOAuthJWTControllerTestDB(t)

	user := &model.User{
		Username:    "self-password-user",
		Password:    "12345678",
		DisplayName: "Self Password User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("self-password-secret"))))
	router.GET("/test/login", func(c *gin.Context) {
		var dbUser model.User
		require.NoError(t, model.DB.First(&dbUser, user.Id).Error)
		session := sessions.Default(c)
		session.Set("id", dbUser.Id)
		session.Set("username", dbUser.Username)
		session.Set("role", dbUser.Role)
		session.Set("status", dbUser.Status)
		session.Set("group", dbUser.Group)
		session.Set(constant.SessionKeyIDEpoch, dbUser.SessionEpoch)
		require.NoError(t, session.Save())
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	router.PUT("/api/user/self", middleware.UserAuth(), UpdateSelf)
	router.GET("/test/me", middleware.UserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	login := func() []*http.Cookie {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test/login", nil))
		cookies := recorder.Result().Cookies()
		require.NotEmpty(t, cookies)
		return cookies
	}
	withCookies := func(req *http.Request, cookies []*http.Cookie) *httptest.ResponseRecorder {
		for _, sessionCookie := range cookies {
			req.AddCookie(sessionCookie)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	currentDevice := login()
	otherDevice := login()

	body := `{"username":"` + user.Username + `","display_name":"Self Password User",` +
		`"original_password":"12345678","password":"87654321"}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/user/self", strings.NewReader(body))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("New-Api-User", strconv.Itoa(user.Id))
	update := withCookies(updateReq, currentDevice)
	require.Contains(t, update.Body.String(), `"success":true`, update.Body.String())
	// 后续请求要使用改密响应里刷新的 Cookie（同名 Cookie 只保留新的）。
	refreshed := update.Result().Cookies()
	require.NotEmpty(t, refreshed, "改密响应必须下发刷新后的会话 Cookie")
	currentDevice = refreshed

	meReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/test/me", nil)
		req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
		return req
	}
	require.Equal(t, http.StatusOK, withCookies(meReq(), currentDevice).Code,
		"改密码的当前会话必须继续可用")

	require.Equal(t, http.StatusUnauthorized, withCookies(meReq(), otherDevice).Code,
		"其它设备上的旧会话必须被吊销")
}
