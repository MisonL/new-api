package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetHealthz reports HTTP liveness without checking external dependencies.
func GetHealthz(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}
