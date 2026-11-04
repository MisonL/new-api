package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// GetLogsSelfStat 按 session 里的 username 过滤日志聚合。改名或用户名被
// 复用后，同名用户的历史日志会被计入别人的统计（行级权限仍是 user_id）。
// 当前用户已有 id，必须用它作第一道过滤。
func TestGetLogsSelfStatScopesByUserID(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t, &model.Log{})
	db := model.LOG_DB

	// 两个用户共用同一个 username（改名场景）。
	require.NoError(t, db.Create(&model.Log{UserId: 41, Username: "shared", Type: 2, Quota: 100, CreatedAt: 1}).Error)
	require.NoError(t, db.Create(&model.Log{UserId: 42, Username: "shared", Type: 2, Quota: 200, CreatedAt: 2}).Error)

	// 以用户 41 的会话查询，只应聚合到 41 的日志。
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/stat?type=2", nil)
	c.Set("username", "shared")
	c.Set("id", 41)

	GetLogsSelfStat(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Data struct {
			Quota int64 `json:"quota"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, int64(100), body.Data.Quota,
		"统计必须按当前用户 id 隔离，不能混入同名用户的日志")
}
