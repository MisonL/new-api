package controller

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 同一浏览器里 A 完成安全验证后，B 登录必须拿不到 A 的 step-up 状态：
// secure_verified_at / secure_verified_method 是会话级凭证，不属于任何用户，
// 换账号重新签发会话时不清除，B 在 300 秒内就能免验证执行敏感操作。
func TestSetupLoginClearsSecureVerificationState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.KeyColumn()
	t.Cleanup(func() {
		model.DB = previousDB
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TwoFA{}))

	user := &model.User{
		Id:       77,
		Username: "secure-verify-probe",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Quota:    1000,
		Group:    "default",
	}
	require.NoError(t, model.DB.Create(user).Error)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("secure-verify-test-secret"))))
	router.GET("/set-verification", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(middleware.SecureVerificationSessionKey, int64(1))
		session.Set("secure_verified_method", "passkey")
		session.Set("id", 999) // 另一个用户留下的会话
		if err := session.Save(); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/login", func(c *gin.Context) {
		setupLogin(user, c)
	})
	router.GET("/probe", func(c *gin.Context) {
		session := sessions.Default(c)
		verifiedAt := session.Get(middleware.SecureVerificationSessionKey)
		verifiedMethod := session.Get("secure_verified_method")
		sessionID := session.Get("id")
		c.JSON(http.StatusOK, gin.H{
			"verified_at":     verifiedAt,
			"verified_method": verifiedMethod,
			"id":              sessionID,
		})
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar}

	request := func(path string) map[string]any {
		resp, err := client.Get(srv.URL + path)
		require.NoError(t, err)
		defer resp.Body.Close()
		var payload map[string]any
		if err := common.DecodeJson(resp.Body, &payload); err != nil {
			// 204 无正文，返回空 map
			return map[string]any{}
		}
		return payload
	}

	request("/set-verification")
	request("/login")
	payload := request("/probe")

	require.Nil(t, payload["verified_at"], "登录后不得保留上一个用户的 secure_verified_at")
	require.Nil(t, payload["verified_method"], "登录后不得保留上一个用户的 secure_verified_method")
	require.EqualValues(t, user.Id, payload["id"], "会话应切换到新登录的用户")
}
