package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHealthzRouteBypassesAPIRateLimit(t *testing.T) {
	previousEnabled := common.GlobalApiRateLimitEnable
	previousLimit := common.GlobalApiRateLimitNum
	previousRedisEnabled := common.RedisEnabled
	common.GlobalApiRateLimitEnable = true
	common.GlobalApiRateLimitNum = 0
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.GlobalApiRateLimitEnable = previousEnabled
		common.GlobalApiRateLimitNum = previousLimit
		common.RedisEnabled = previousRedisEnabled
	})

	engine := gin.New()
	SetApiRouter(engine)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)

	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "ok", recorder.Body.String())
}
