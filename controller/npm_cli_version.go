package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelNpmVersionOptions(c *gin.Context) {
	packageName := c.Query("package")
	response, err := service.FetchNpmCLIVersionOptionsResponse(c.Request.Context(), packageName)
	if err != nil {
		writeNpmVersionOptionsError(c, packageName, "unavailable", err)
		return
	}
	common.ApiSuccess(c, response)
}

func RefreshChannelNpmVersionOptions(c *gin.Context) {
	packageName := c.Query("package")
	response, err := service.RefreshNpmCLIVersionOptionsResponse(c.Request.Context(), packageName)
	if err != nil {
		writeNpmVersionOptionsError(c, packageName, npmVersionOptionsErrorSource("npm", err), err)
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf(
		"npm cli version options refreshed: package=%q source=%s latest_version=%q options=%d",
		response.PackageName,
		response.Source,
		response.LatestVersion,
		len(response.Options),
	))
	common.ApiSuccess(c, response)
}

func GetChannelNpmVersionDiagnostics(c *gin.Context) {
	common.ApiSuccess(c, service.GetNpmCLIVersionDiagnosticsWithContext(c.Request.Context()))
}

func writeNpmVersionOptionsError(c *gin.Context, packageName string, source string, err error) {
	code := service.NpmCLIVersionErrorCode(err)
	message := fmt.Sprintf(
		"npm cli version options failed: package=%q source=%s code=%s error=%q",
		packageName,
		source,
		code,
		err.Error(),
	)
	if code == service.NpmCLIPackageRequiredCode ||
		code == service.NpmCLIPackageUnsupportedCode ||
		code == service.NpmCLIVersionNotRecordedCode {
		logger.LogInfo(c.Request.Context(), message)
	} else {
		logger.LogWarn(c.Request.Context(), message)
	}
	c.JSON(200, gin.H{
		"success": false,
		"message": service.NpmCLIVersionPublicErrorMessage(err),
		"code":    code,
		"data": gin.H{
			"package": packageName,
			"source":  source,
			"code":    code,
		},
	})
}

func npmVersionOptionsErrorSource(defaultSource string, err error) string {
	if service.NpmCLIVersionErrorCode(err) == service.NpmCLIVersionPersistFailedCode {
		return "persist"
	}
	return defaultSource
}
