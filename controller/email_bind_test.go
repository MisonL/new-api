package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newEmailBindTestRouter wires a session middleware, a login-as helper and the
// real EmailBind handler, so the test drives the handler exactly as the API does.
func newEmailBindTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	router := gin.New()
	store := cookie.NewStore([]byte("email-bind-test-secret"))
	router.Use(sessions.Sessions("session", store))
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
		if err := session.Save(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	router.POST("/api/oauth/email/bind", EmailBind)
	return router
}

func createEmailBindTestUser(t *testing.T, id int, username string, email string) *model.User {
	t.Helper()
	user := &model.User{
		Id:          id,
		Username:    username,
		Password:    "password123",
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     fmt.Sprintf("aff-%d", id),
	}
	if email != "" {
		user.Email = email
	}
	require.NoError(t, model.DB.Create(user).Error)
	return user
}

func performEmailBindRequest(t *testing.T, client *http.Client, baseURL string, email string, code string) oauthJWTAPIResponse {
	t.Helper()
	body, err := common.Marshal(map[string]string{"email": email, "code": code})
	require.NoError(t, err)
	resp, err := client.Post(baseURL+"/api/oauth/email/bind", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	var payload oauthJWTAPIResponse
	require.NoError(t, common.DecodeJson(resp.Body, &payload))
	return payload
}

// The email is only checked for availability when the verification code is
// sent. The code stays valid for ten minutes, during which the address can be
// registered by someone else. Because the email column is not unique, a second
// row with the same address would let ResetUserPasswordByEmail rewrite the
// password of every account with that address and hand the plaintext back to
// the caller. The handler must therefore re-check availability after verifying
// the code, not only at send time.
func TestEmailBindRejectsEmailTakenDuringCodeWindow(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	router := newEmailBindTestRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	binder := createEmailBindTestUser(t, 4101, "binder", "")
	createEmailBindTestUser(t, 4102, "squatter", "contested@example.com")

	client := newTestHTTPClient(t)
	resp, err := client.Get(server.URL + "/test/login-as/4101")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	code := "123456"
	common.RegisterVerificationCodeWithKey("contested@example.com", code, common.EmailVerificationPurpose)
	t.Cleanup(func() {
		common.DeleteKey("contested@example.com", common.EmailVerificationPurpose)
	})

	result := performEmailBindRequest(t, client, server.URL, "contested@example.com", code)
	require.False(t, result.Success, "binding an address already owned by another account must fail")
	require.NotEmpty(t, result.Message)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, binder.Id).Error)
	require.Empty(t, reloaded.Email, "the failed bind must not persist the contested email")
}

// Control: an unclaimed address still binds, so the guard above is not simply
// rejecting every request.
func TestEmailBindAcceptsUnclaimedEmail(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	router := newEmailBindTestRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	binder := createEmailBindTestUser(t, 4201, "binder2", "")

	client := newTestHTTPClient(t)
	resp, err := client.Get(server.URL + "/test/login-as/4201")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	code := "654321"
	common.RegisterVerificationCodeWithKey("fresh@example.com", code, common.EmailVerificationPurpose)
	t.Cleanup(func() {
		common.DeleteKey("fresh@example.com", common.EmailVerificationPurpose)
	})

	result := performEmailBindRequest(t, client, server.URL, "fresh@example.com", code)
	require.True(t, result.Success, "an unclaimed address must bind successfully: %s", result.Message)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, binder.Id).Error)
	require.Equal(t, "fresh@example.com", reloaded.Email)
}

// 账户已经持有该地址时再次绑定同一地址是幂等确认，不能判成冲突。OAuth 登录
// 会把 IdP 的邮箱同步到本地账户，用户随后走一次绑定确认就会命中这条路径：
// 不排除调用者自己那一行的占用检查会把自己判成占用者，报"该邮箱已被其他账户
// 绑定或注册"，而那个"其他账户"就是请求者本人。
func TestEmailBindAllowsRebindingOwnEmail(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	router := newEmailBindTestRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	const ownEmail = "self@example.com"
	binder := createEmailBindTestUser(t, 4301, "selfbinder", ownEmail)

	client := newTestHTTPClient(t)
	resp, err := client.Get(server.URL + "/test/login-as/4301")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	code := "112233"
	common.RegisterVerificationCodeWithKey(ownEmail, code, common.EmailVerificationPurpose)
	t.Cleanup(func() {
		common.DeleteKey(ownEmail, common.EmailVerificationPurpose)
	})

	result := performEmailBindRequest(t, client, server.URL, ownEmail, code)
	require.True(t, result.Success,
		"rebinding the caller's own address must not be reported as a conflict: %s", result.Message)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, binder.Id).Error)
	require.Equal(t, ownEmail, reloaded.Email)
}

// 排除自己那一行不能顺手把"别人占用"也放过：同一地址被第三个账户持有时，
// 绑定仍必须失败。
func TestEmailBindStillRejectsAddressHeldByAnotherAccount(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	router := newEmailBindTestRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	const shared = "shared@example.com"
	createEmailBindTestUser(t, 4401, "firstowner", shared)
	binder := createEmailBindTestUser(t, 4402, "latcomer", "")

	client := newTestHTTPClient(t)
	resp, err := client.Get(server.URL + "/test/login-as/4402")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	code := "445566"
	common.RegisterVerificationCodeWithKey(shared, code, common.EmailVerificationPurpose)
	t.Cleanup(func() {
		common.DeleteKey(shared, common.EmailVerificationPurpose)
	})

	result := performEmailBindRequest(t, client, server.URL, shared, code)
	require.False(t, result.Success, "an address held by another account must still be rejected")

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, binder.Id).Error)
	require.Empty(t, reloaded.Email, "the rejected bind must not persist the address")
}
