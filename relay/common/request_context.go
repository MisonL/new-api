package common

import (
	"context"

	"github.com/gin-gonic/gin"
)

func GinRequestContext(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}
