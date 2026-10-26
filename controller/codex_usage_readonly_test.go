package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// GetCodexChannelUsage 是 GET /api/channel/:id/codex/usage，语义上是一次
// 只读查询。旧实现在上游 401/403 时顺手刷新并持久化 OAuth token，还会
// 重建渠道缓存、清空代理客户端缓存：一个查询接口产生了写库副作用，
// 也让独立的"刷新凭证"入口形同虚设。
//
// 两个断言各自守一条契约：
//  1. 401 时响应要明确指向"刷新凭证"这个动作，而不是把裸 upstream status
//     丢给前端——这条在旧实现下失败，是本用例的 RED。
//  2. key 不得被改写——刷新失败时旧实现本来也不写，这条守的是"将来有人
//     把刷新重新塞回 GET"的回归。
func TestGetCodexChannelUsageDoesNotRefreshOnUnauthorized(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer upstream.Close()

	db := setupChannelControllerTestDB(t)

	keyJSON, err := common.Marshal(map[string]any{
		"access_token":  "stale-access-token",
		"refresh_token": "refresh-token",
		"account_id":    "acct-1",
		"type":          "codex",
	})
	require.NoError(t, err)
	baseURL := upstream.URL
	channel := model.Channel{
		Id:      8001,
		Type:    constant.ChannelTypeCodex,
		Name:    "codex-readonly",
		Key:     string(keyJSON),
		BaseURL: &baseURL,
	}
	require.NoError(t, db.Create(&channel).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/channel/%d/codex/usage", channel.Id), nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.Id)}}

	GetCodexChannelUsage(c)

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.False(t, resp.Success)
	require.Contains(t, resp.Message, "刷新凭证",
		"a GET that cannot refresh must tell the caller to use the refresh action, got: %q", resp.Message)

	var stored model.Channel
	require.NoError(t, db.First(&stored, "id = ?", channel.Id).Error)
	require.Equal(t, string(keyJSON), stored.Key,
		"a GET usage lookup must not rewrite the channel credential")
}
