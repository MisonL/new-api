package router

import (
	"embed"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

var webRootStaticExtensions = map[string]struct{}{
	".css":         {},
	".gif":         {},
	".ico":         {},
	".jpeg":        {},
	".jpg":         {},
	".js":          {},
	".json":        {},
	".map":         {},
	".m4a":         {},
	".mjs":         {},
	".mp3":         {},
	".mp4":         {},
	".ogg":         {},
	".otf":         {},
	".png":         {},
	".svg":         {},
	".ttf":         {},
	".txt":         {},
	".webmanifest": {},
	".webm":        {},
	".webp":        {},
	".woff":        {},
	".woff2":       {},
	".xml":         {},
}

// ThemeAssets holds the embedded frontend assets for both themes.
type ThemeAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
	ClassicBuildFS   embed.FS
	ClassicIndexPage []byte
}

func SetWebRouter(router *gin.Engine, assets ThemeAssets) {
	defaultFS := common.EmbedFolder(assets.DefaultBuildFS, "web/default/dist")
	classicFS := common.EmbedFolder(assets.ClassicBuildFS, "web/classic/dist")
	themeFS := common.NewThemeAwareFS(defaultFS, classicFS)

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	router.Use(static.Serve("/", themeFS))
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		if shouldReturnRelayNotFound(c.Request.RequestURI, c.Request.URL.Path) {
			controller.RelayNotFound(c)
			return
		}
		c.Header("Cache-Control", "no-cache")
		if common.GetTheme() == "classic" {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.ClassicIndexPage)
		} else {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.DefaultIndexPage)
		}
	})
}

func shouldReturnRelayNotFound(requestURI string, requestPath string) bool {
	return strings.HasPrefix(requestURI, "/v1") ||
		strings.HasPrefix(requestURI, "/api") ||
		middleware.IsWebStaticResourcePath(requestPath) ||
		isRootWebStaticFilePath(requestPath)
}

func isRootWebStaticFilePath(requestPath string) bool {
	trimmed := strings.TrimPrefix(requestPath, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return false
	}
	_, ok := webRootStaticExtensions[strings.ToLower(filepath.Ext(trimmed))]
	return ok
}
