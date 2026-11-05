package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 多密钥渠道所有 key 都不可用时，SetupContextForSelectedChannel 返回
// ErrorCodeChannelNoAvailableKey。relay 的重试循环会排除该渠道后继续，但
// distributor 的选渠发生在这个循环之前：按原实现请求会直接以 500 结束，
// 同一分组里健康的渠道永远拿不到这次请求。自动选渠的请求必须换渠道重试；
// token 显式钉住的渠道仍应原样失败。
func TestDistributeFailsOverWhenSelectedChannelHasNoEnabledKey(t *testing.T) {
	db := setupDistributorBootstrapTestDB(t)

	broken := &model.Channel{
		Id:       2401,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "broken-key-1\nbroken-key-2",
		Status:   common.ChannelStatusEnabled,
		Name:     "broken-multikey-channel",
		Models:   "gpt-5.5",
		Group:    "default",
		Priority: common.GetPointer(int64(10)),
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModeRandom,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, db.Create(broken).Error)
	require.NoError(t, broken.AddAbilities(nil))

	healthy := &model.Channel{
		Id:     2402,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "healthy-key",
		Status: common.ChannelStatusEnabled,
		Name:   "healthy-channel",
		Models: "gpt-5.5",
		Group:  "default",
	}
	require.NoError(t, db.Create(healthy).Error)
	require.NoError(t, healthy.AddAbilities(nil))
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		c.Next()
	})
	selectedChannelID := 0
	router.POST("/v1/chat/completions", Distribute(), func(c *gin.Context) {
		selectedChannelID = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.5"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code,
		"首选渠道没有可用 key 时必须换渠道，不能直接失败：%s", recorder.Body.String())
	require.Equal(t, healthy.Id, selectedChannelID, "请求必须落到健康渠道")
}

// 反向对照：没有可用渠道时仍应明确失败，且不能因为重试变成成功。
func TestDistributeStillFailsWhenNoChannelHasEnabledKey(t *testing.T) {
	db := setupDistributorBootstrapTestDB(t)

	broken := &model.Channel{
		Id:       2411,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "only-broken-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "only-broken-channel",
		Models:   "gpt-5.5",
		Group:    "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 1,
			MultiKeyMode: constant.MultiKeyModeRandom,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, db.Create(broken).Error)
	require.NoError(t, broken.AddAbilities(nil))
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		c.Next()
	})
	router.POST("/v1/chat/completions", Distribute(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.5"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.NotEqual(t, http.StatusOK, recorder.Code, "没有任何可用 key 的请求不得成功")
}
