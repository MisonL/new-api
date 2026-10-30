package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// SyncUpstreamPreview 只用 `_ =` 读本地模型与供应商：查询失败时 locals 为空，
// 预览会把上游模型全部报成"缺失"，运营照着这个结果导入会给每条已有模型再建
// 一份。本地查询失败必须显式报错，而不是把"查不出来"当成"本地没有"。
func TestSyncUpstreamPreviewReportsLocalQueryFailure(t *testing.T) {
	db := setupHeaderPolicyControllerTestDB(t, &model.Model{}, &model.Vendor{})
	// 上游数据正常返回一条模型；本地查询注定失败（表被删）。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/newapi/vendors.json" {
			_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":[{"model_name":"probe-sync-model"}]}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("SYNC_UPSTREAM_BASE", server.URL)

	require.NoError(t, db.Migrator().DropTable(&model.Model{}))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/model/sync/preview", nil)

	SyncUpstreamPreview(c)

	require.Contains(t, recorder.Body.String(), `"success":false`,
		"a failed local query must not be reported as a successful preview")
	require.NotContains(t, recorder.Body.String(), "probe-sync-model",
		"a failed local query must not present every upstream model as missing")
}
