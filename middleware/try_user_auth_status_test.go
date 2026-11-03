package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TryUserAuth 只把会话里的 id 透传出去，不校验用户状态。禁用用户后，会话
// Cookie 的有效期内他仍能带着旧 id 访问 /api/pricing、/api/user/groups 等
// 接口，并拿到按自己分组计算的价格与可见分组。这里要求它和其它会话分支
// （TokenOrUserAuth / authHelper）一致：回读身份、状态非启用时按未登录处理。
func TestTryUserAuthRejectsDisabledUserSession(t *testing.T) {
	setupAuthMiddlewareTestDB(t)

	user := &model.User{
		Username:    "try-user-auth-disabled",
		Password:    "12345678",
		DisplayName: "Try User Auth Disabled",
		Role:        1,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	router := gin.New()
	store := cookie.NewStore([]byte("try-user-auth-secret"))
	router.Use(sessions.Sessions("session", store))
	router.GET("/test/login/:id", func(c *gin.Context) {
		userID, _ := strconv.Atoi(c.Param("id"))
		dbUser, err := model.GetUserById(userID, false)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
			return
		}
		session := sessions.Default(c)
		session.Set("id", dbUser.Id)
		session.Set("username", dbUser.Username)
		session.Set("role", dbUser.Role)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", dbUser.Group)
		require.NoError(t, session.Save())
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	router.GET("/test/pricing", TryUserAuth(), func(c *gin.Context) {
		id, exists := c.Get("id")
		c.JSON(http.StatusOK, gin.H{"success": true, "id": id, "idSet": exists})
	})

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/test/login/%d", user.Id), nil))
	cookies := login.Result().Cookies()
	require.NotEmpty(t, cookies)

	fetch := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/test/pricing", nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	enabled := fetch()
	require.Equal(t, http.StatusOK, enabled.Code)
	require.Contains(t, enabled.Body.String(), `"idSet":true`,
		"启用用户的会话必须暴露 id：%s", enabled.Body.String())

	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).
		Update("status", common.UserStatusDisabled).Error)

	disabled := fetch()
	require.Equal(t, http.StatusOK, disabled.Code,
		"TryUserAuth 是可选鉴权，禁用后仍应放行，只是按未登录处理")
	require.Contains(t, disabled.Body.String(), `"idSet":false`,
		"已禁用用户的会话不得再暴露 id：%s", disabled.Body.String())
}
