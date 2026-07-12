package middleware

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const RouteTagKey = "route_tag"

const accessLogRedactedValue = "REDACTED"

func sanitizeAccessLogPath(path string) string {
	queryIndex := strings.IndexByte(path, '?')
	if queryIndex < 0 {
		return path
	}
	basePath := path[:queryIndex]
	values, err := url.ParseQuery(path[queryIndex+1:])
	if err != nil {
		return basePath + "?" + accessLogRedactedValue
	}
	for key, items := range values {
		for i := range items {
			items[i] = accessLogRedactedValue
		}
		values[key] = items
	}
	if len(values) == 0 {
		return basePath
	}
	return basePath + "?" + values.Encode()
}

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

func SetUpLogger(server *gin.Engine) {
	server.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		var requestID string
		if param.Keys != nil {
			requestID, _ = param.Keys[common.RequestIdKey].(string)
		}
		tag, _ := param.Keys[RouteTagKey].(string)
		if tag == "" {
			tag = "web"
		}
		return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			tag,
			requestID,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			sanitizeAccessLogPath(param.Path),
		)
	}))
}
