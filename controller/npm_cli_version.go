package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelNpmVersionOptions(c *gin.Context) {
	options, err := service.FetchNpmCLIVersionOptions(c.Request.Context(), c.Query("package"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, options)
}
