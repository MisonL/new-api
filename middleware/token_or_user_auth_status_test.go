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

// TokenOrUserAuth 的会话分支必须回读数据库校验用户状态，不能只信会话里的
// status 快照：管理员禁用用户后，只要该用户不再触发任何 UserAuth 路径刷新
// 会话，会话 Cookie 的 30 天有效期内他仍能访问受保护的只读接口（视频内容）。
func TestTokenOrUserAuthRejectsStaleDisabledSession(t *testing.T) {
	setupAuthMiddlewareTestDB(t)

	user := &model.User{
		Username:    "stale-disabled-user",
		Password:    "12345678",
		DisplayName: "Stale Disabled User",
		Role:        1,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	router := gin.New()
	store := cookie.NewStore([]byte("token-or-user-auth-secret"))
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
		// 会话快照写成启用，模拟"禁用前签发、之后未刷新"的会话。
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", dbUser.Group)
		require.NoError(t, session.Save())
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	router.GET("/test/content", TokenOrUserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "id": c.GetInt("id")})
	})

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/test/login/%d", user.Id), nil))
	cookies := login.Result().Cookies()
	require.NotEmpty(t, cookies)

	fetch := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/test/content", nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	enabled := fetch()
	require.Equal(t, http.StatusOK, enabled.Code, enabled.Body.String())

	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).
		Update("status", common.UserStatusDisabled).Error)

	disabled := fetch()
	require.Equal(t, http.StatusUnauthorized, disabled.Code,
		"stale enabled session must not survive a DB disable: %s", disabled.Body.String())
}
