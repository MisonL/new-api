package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// UpsertPasskeyCredential 是替换而非追加：它先按 user_id 硬删再插入。因此
// "注册"对已绑定账户等同于换绑第二因子，与删除 Passkey 同级，必须 step-up。
// 门禁若在未开 2FA 时直接放行，任何拿到会话的人都能用自己的认证器顶掉
// 受害者原有的 Passkey，并借此长期免密登录（改密码也不失效）。

func setupPasskeyRegistrationGateTest(t *testing.T) *gin.Engine {
	t.Helper()
	setupHeaderPolicyControllerTestDB(t, &model.User{}, &model.TwoFA{}, &model.PasskeyCredential{})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("passkey-gate-test-secret"))))
	router.POST("/register/begin", func(c *gin.Context) {
		// 与 PasskeyRegisterBegin 相同：先取会话用户，再过门禁。
		userID := c.GetInt("id")
		if userID == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false})
			return
		}
		if !requirePasskeyRegistrationVerification(c, userID) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "gate passed"})
	})
	router.GET("/mark-verified/:method", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(SecureVerificationSessionKey, time.Now().Unix())
		session.Set(secureVerificationMethodSessionKey, c.Param("method"))
		_ = session.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	return router
}

func seedPasskeyGateUser(t *testing.T, userID int, with2FA bool, withPasskey bool) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{
		Id:       userID,
		Username: "passkey-gate-" + string(rune('a'+userID%26)),
		Status:   common.UserStatusEnabled,
		Quota:    100,
	}).Error)

	if with2FA {
		require.NoError(t, model.DB.Create(&model.TwoFA{
			UserId:    userID,
			Secret:    "JBSWY3DPEHPK3PXP",
			IsEnabled: true,
		}).Error)
	}
	if withPasskey {
		require.NoError(t, model.DB.Create(&model.PasskeyCredential{
			UserID:       userID,
			CredentialID: "gate-cred-" + string(rune('a'+userID%26)),
			PublicKey:    "pk",
		}).Error)
	}
}

type passkeyGateResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// 首次绑定（无 Passkey、无 2FA）必须放行，否则新用户无法启用 Passkey。
func TestPasskeyRegistrationGateAllowsFirstEnrollment(t *testing.T) {
	setupPasskeyRegistrationGateTest(t)
	seedPasskeyGateUser(t, 9101, false, false)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("passkey-gate-test-secret"))))
	router.POST("/register/begin", func(c *gin.Context) {
		c.Set("id", 9101)
		if !requirePasskeyRegistrationVerification(c, 9101) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "gate passed"})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/register/begin", nil))

	var result passkeyGateResult
	require.NoError(t, common.UnmarshalJsonStr(recorder.Body.String(), &result))
	require.True(t, result.Success, "first enrollment must not require step-up: %+v", result)
}

// 已绑定 Passkey 且未开 2FA：注册会替换现有凭证，必须要求 step-up。
func TestPasskeyRegistrationGateRequiresStepUpWhenReplacingExistingPasskey(t *testing.T) {
	setupPasskeyRegistrationGateTest(t)
	seedPasskeyGateUser(t, 9102, false, true)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("passkey-gate-test-secret"))))
	router.POST("/register/begin", func(c *gin.Context) {
		c.Set("id", 9102)
		if !requirePasskeyRegistrationVerification(c, 9102) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "gate passed"})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/register/begin", nil))

	var result passkeyGateResult
	require.NoError(t, common.UnmarshalJsonStr(recorder.Body.String(), &result))
	require.False(t, result.Success,
		"replacing an existing passkey must require step-up, otherwise a stolen session can swap the credential")
}

// 已绑定 Passkey 且通过了 passkey step-up：放行。
func TestPasskeyRegistrationGateAllowsAfterPasskeyStepUp(t *testing.T) {
	setupPasskeyRegistrationGateTest(t)
	seedPasskeyGateUser(t, 9103, false, true)

	store := cookie.NewStore([]byte("passkey-gate-test-secret"))
	router := gin.New()
	router.Use(sessions.Sessions("session", store))
	router.GET("/mark", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(SecureVerificationSessionKey, time.Now().Unix())
		session.Set(secureVerificationMethodSessionKey, secureVerificationMethodPasskey)
		require.NoError(t, session.Save())
		c.Status(http.StatusOK)
	})
	router.POST("/register/begin", func(c *gin.Context) {
		c.Set("id", 9103)
		if !requirePasskeyRegistrationVerification(c, 9103) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "gate passed"})
	})

	markRecorder := httptest.NewRecorder()
	router.ServeHTTP(markRecorder, httptest.NewRequest(http.MethodGet, "/mark", nil))
	require.Equal(t, http.StatusOK, markRecorder.Code)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/register/begin", nil)
	for _, cookieValue := range markRecorder.Result().Cookies() {
		req.AddCookie(cookieValue)
	}
	router.ServeHTTP(recorder, req)

	var result passkeyGateResult
	require.NoError(t, common.UnmarshalJsonStr(recorder.Body.String(), &result))
	require.True(t, result.Success, "a completed passkey step-up must satisfy the gate: %+v", result)
}

// 开了 2FA 时，2FA step-up 满足门禁；没有 step-up 则拒绝。
func TestPasskeyRegistrationGateRequiresTwoFAWhenEnabled(t *testing.T) {
	setupPasskeyRegistrationGateTest(t)
	seedPasskeyGateUser(t, 9104, true, false)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("passkey-gate-test-secret"))))
	router.POST("/register/begin", func(c *gin.Context) {
		c.Set("id", 9104)
		if !requirePasskeyRegistrationVerification(c, 9104) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "gate passed"})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/register/begin", nil))

	var result passkeyGateResult
	require.NoError(t, common.UnmarshalJsonStr(recorder.Body.String(), &result))
	require.False(t, result.Success, "2FA-enabled accounts must complete 2FA step-up")
}
