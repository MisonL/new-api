package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// SystemPerformanceCheck 检查系统性能中间件
func SystemPerformanceCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/v1/messages") {
			if err := checkSystemPerformance(); err != nil {
				c.JSON(err.StatusCode, gin.H{
					"error": err.ToClaudeError(),
				})
				c.Abort()
				return
			}
		} else {
			if err := checkSystemPerformance(); err != nil {
				c.JSON(err.StatusCode, gin.H{
					"error": err.ToOpenAIError(),
				})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// checkSystemPerformance 检查系统性能是否超过阈值
func checkSystemPerformance() *types.NewAPIError {
	// 过载标记在采样侧完成：CPU / 内存按容器 cgroup 配额计算，并要求连续多次
	// 超阈值；判定时再按当前阈值复核，并忽略过期采样。
	overload := common.CheckSystemOverload(common.GetSystemStatus(), common.GetPerformanceMonitorConfig(), time.Now())
	if overload == nil {
		return nil
	}
	return types.NewErrorWithStatusCode(overload, types.ErrorCode(overload.Code), http.StatusServiceUnavailable)
}
