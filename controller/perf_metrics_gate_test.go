package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// perf_metrics_setting.enabled 只挡写入与清洗时，两个读接口在关闭后仍可匿名
// 拉取全部内部分组的模型性能数据（第十轮运行态发现）。开关必须贯通到读路径：
// 关闭后两个接口都返回 404，与前端"关闭即隐藏入口"的行为一致。
func TestPerfMetricsReadEndpointsRespectDisabledSetting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupHeaderPolicyControllerTestDB(t)

	// 与生产写路径同一条入口：option 表的 JSON 经 UpdateConfigFromMap 落到
	// 注册的设置结构上。
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("perf_metrics_setting"),
		map[string]string{"enabled": "false"}))
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("perf_metrics_setting"),
			map[string]string{"enabled": "true"}))
	})
	require.False(t, perf_metrics_setting.GetSetting().Enabled, "前置条件：开关应已关闭")

	summaryRecorder := httptest.NewRecorder()
	summaryContext, _ := gin.CreateTestContext(summaryRecorder)
	summaryContext.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics/summary?hours=24", nil)
	GetPerfMetricsSummary(summaryContext)
	require.Equal(t, http.StatusNotFound, summaryRecorder.Code,
		"开关关闭时 summary 必须拒绝：%s", summaryRecorder.Body.String())

	metricsRecorder := httptest.NewRecorder()
	metricsContext, _ := gin.CreateTestContext(metricsRecorder)
	metricsContext.Request = httptest.NewRequest(http.MethodGet, "/api/perf-metrics?model=gpt-5.4", nil)
	GetPerfMetrics(metricsContext)
	require.Equal(t, http.StatusNotFound, metricsRecorder.Code,
		"开关关闭时明细接口必须拒绝：%s", metricsRecorder.Body.String())
}
