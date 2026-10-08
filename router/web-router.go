package router

import (
	"embed"
	"net/http"
	"path"
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
	if err := validateFrontendReleaseAssets(assets.DefaultBuildFS, assets.ClassicBuildFS); err != nil {
		common.FatalLog("frontend release assets validation failed: " + err.Error())
	}

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

// shouldReturnRelayNotFound decides whether an unmatched path answers with
// the relay 404 JSON instead of the SPA index page. It classifies both the
// raw request URI and the normalized path, because a probe spelling like
// "//api/status", "/./api/status" or "/%2f%2fapi/status" matches no route and
// would otherwise get a 200 HTML SPA body, breaking the /api and /v1 JSON 404
// contract and misleading status-code-only clients. The union keeps paths
// such as "/assets/../console" on the branch they already take. Matching
// stays case-sensitive because routes are.
func shouldReturnRelayNotFound(requestURI string, requestPath string) bool {
	normalizedPath := path.Clean(requestPath)
	return isRelayNotFoundPath(requestURI) || isRelayNotFoundPath(normalizedPath)
}

// isRelayNotFoundPath reports whether a path or request URI belongs to a
// route family that must answer with the relay 404 JSON rather than the SPA
// fallback.
func isRelayNotFoundPath(pathOrURI string) bool {
	return strings.HasPrefix(pathOrURI, "/v1") ||
		strings.HasPrefix(pathOrURI, "/api") ||
		middleware.IsWebStaticResourcePath(pathOrURI) ||
		isRootWebStaticFilePath(pathOrURI)
}

func isRootWebStaticFilePath(requestPath string) bool {
	trimmed := strings.TrimPrefix(requestPath, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return false
	}
	_, ok := webRootStaticExtensions[strings.ToLower(filepath.Ext(trimmed))]
	return ok
}
