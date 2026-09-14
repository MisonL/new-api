package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetChannelHealthRuntimeStatus exposes the bounded administrator runtime view
// for temporary channel cooling and half-open probes.
func GetChannelHealthRuntimeStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    service.GetChannelHealthRuntimeStatus(),
	})
}
