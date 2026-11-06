package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 两个并发注册请求使用同一邮箱（用户名不同）。旧实现先查后插，邮箱非唯一
// 索引，两个请求都能通过 CheckUserExistOrDeleted 并各自落库；随后按邮箱
// 重置密码会同时改写两个账户。注册必须把检查与落库串行化。
func TestRegisterDoesNotCreateDuplicateEmailConcurrently(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	gin.SetMode(gin.TestMode)

	originalRedisEnabled := common.RedisEnabled
	originalEmailVerification := common.EmailVerificationEnabled
	originalGenerateDefaultToken := constant.GenerateDefaultToken
	common.RedisEnabled = false
	common.EmailVerificationEnabled = true
	constant.GenerateDefaultToken = false
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.EmailVerificationEnabled = originalEmailVerification
		constant.GenerateDefaultToken = originalGenerateDefaultToken
	})

	router := gin.New()
	router.POST("/api/user/register", Register)

	const email = "race-shared@example.com"
	common.RegisterVerificationCodeWithKey(email, "123456", common.EmailVerificationPurpose)
	t.Cleanup(func() {
		common.DeleteKey(email, common.EmailVerificationPurpose)
	})

	start := make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, 2)
	bodies := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			body := `{"username":"race-user-` + []string{"a", "b"}[index] + `","password":"12345678","email":"` + email + `","verification_code":"123456"}`
			req := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			<-start
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			codes[index] = recorder.Code
			bodies[index] = recorder.Body.String()
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range codes {
		require.Equal(t, http.StatusOK, codes[i], bodies[i])
	}

	var count int64
	require.NoError(t, model.DB.Unscoped().Model(&model.User{}).Where("email = ?", email).Count(&count).Error)
	require.Equal(t, int64(1), count, "concurrent registrations must not both claim the same email")
}
