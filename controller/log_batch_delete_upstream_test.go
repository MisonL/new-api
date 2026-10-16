package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 「按条件删除」的过滤字段必须与列表查询同口径。upstream_request_id 是列表
// 侧支持、删除侧曾缺失的字段：只按上游请求 ID 筛选时列表显示匹配行，而删除
// 请求要么被"至少一个过滤条件"拒绝，要么退化为时间窗内全删——破坏性操作的
// 范围超出用户所见（第十轮 P2）。
func seedUpstreamLogProbe(t *testing.T) (int64, int64) {
	t.Helper()
	setupHeaderPolicyControllerTestDB(t, &model.Log{}, &model.User{})
	model.KeyColumn()
	model.LOG_DB = model.DB

	mk := func(upstream string) *model.Log {
		return &model.Log{
			UserId:            7,
			Type:              model.LogTypeConsume,
			ModelName:         "gpt-5.4",
			UpstreamRequestId: upstream,
			Quota:             10,
		}
	}
	require.NoError(t, model.DB.Create(mk("upstream-A")).Error)
	require.NoError(t, model.DB.Create(mk("upstream-B")).Error)
	require.NoError(t, model.DB.Create(mk("")).Error)

	var matching, total int64
	require.NoError(t, model.DB.Model(&model.Log{}).Where("upstream_request_id = ?", "upstream-A").Count(&matching).Error)
	require.NoError(t, model.DB.Model(&model.Log{}).Count(&total).Error)
	require.EqualValues(t, 1, matching, "fixture: one row per upstream id")
	require.EqualValues(t, 3, total, "fixture: three rows total")
	return matching, total
}

func callBatchDeleteAdmin(t *testing.T, body string) *int64 {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/log/batch_delete", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	BatchDeleteLogs(c)
	var resp struct {
		Success bool `json:"success"`
		Data    *struct {
			Deleted int64 `json:"deleted"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp), "body=%s", recorder.Body.String())
	if resp.Success && resp.Data != nil {
		return &resp.Data.Deleted
	}
	return nil
}

func TestBatchDeleteLogsHonoursUpstreamRequestId(t *testing.T) {
	seedUpstreamLogProbe(t)

	deleted := callBatchDeleteAdmin(t, `{"upstream_request_id":"upstream-A"}`)
	require.NotNil(t, deleted, "upstream_request_id 应被接受为有效过滤条件")
	require.EqualValues(t, 1, *deleted, "只删上游请求 ID 匹配的那一行")

	var remaining int64
	require.NoError(t, model.DB.Model(&model.Log{}).Where("upstream_request_id = ?", "upstream-B").Count(&remaining).Error)
	require.EqualValues(t, 1, remaining, "不匹配的行必须保留")
}

func TestBatchDeleteLogsStillRequiresSomeFilter(t *testing.T) {
	seedUpstreamLogProbe(t)

	deleted := callBatchDeleteAdmin(t, `{}`)
	require.Nil(t, deleted, "无过滤条件必须拒绝")
}
