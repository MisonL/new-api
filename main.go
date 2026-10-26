package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	_ "github.com/QuantumNous/new-api/setting/log_retention_setting"
	_ "github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	_ "github.com/QuantumNous/new-api/setting/performance_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	_ "net/http/pprof"
)

//go:embed all:web/default/dist
var buildFS embed.FS

//go:embed web/default/dist/index.html
var indexPage []byte

//go:embed all:web/classic/dist
var classicBuildFS embed.FS

//go:embed web/classic/dist/index.html
var classicIndexPage []byte

func main() {
	startTime := time.Now()

	err := InitResources()
	if err != nil {
		common.FatalLog("failed to initialize resources: " + err.Error())
		return
	}

	common.SysLog("New API " + common.BuildSummary() + " started")
	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
	if common.DebugEnabled {
		common.SysLog("running in debug mode")
	}

	defer func() {
		err := model.CloseDB()
		if err != nil {
			common.FatalLog("failed to close database: " + err.Error())
		}
	}()

	if common.RedisEnabled {
		// for compatibility with old versions
		common.MemoryCacheEnabled = true
	}
	if common.MemoryCacheEnabled {
		common.SysLog("memory cache enabled")
		common.SysLog(fmt.Sprintf("sync frequency: %d seconds", common.SyncFrequency))

		// Add panic recovery and retry for InitChannelCache
		func() {
			defer func() {
				if r := recover(); r != nil {
					common.SysLog(fmt.Sprintf("InitChannelCache panic: %v, retrying once", r))
					// Retry once
					_, _, fixErr := model.FixAbility()
					if fixErr != nil {
						common.FatalLog(fmt.Sprintf("InitChannelCache failed: %s", fixErr.Error()))
					}
				}
			}()
			model.InitChannelCache()
		}()

		go model.SyncChannelCache(common.SyncFrequency)
	}

	// 热更新配置
	go model.SyncOptions(common.SyncFrequency)

	// 数据看板
	go model.UpdateQuotaData()

	if os.Getenv("CHANNEL_UPDATE_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_UPDATE_FREQUENCY"))
		if err != nil {
			common.FatalLog("failed to parse CHANNEL_UPDATE_FREQUENCY: " + err.Error())
		}
		go controller.AutomaticallyUpdateChannels(frequency)
	}

	go controller.AutomaticallyTestChannels()

	// Codex credential auto-refresh check every 10 minutes, refresh when expires within 1 day
	service.StartCodexCredentialAutoRefreshTask()

	// NPM CLI version options are refreshed in background so UI and latest profiles do not depend on request-time registry fetches.
	service.StartNpmCLIVersionRefreshTask()

	// Subscription quota reset task (daily/weekly/monthly/custom)
	service.StartSubscriptionQuotaResetTask()

	// Prune expired synthetic compact state records persisted as Redis/process restart fallback.
	service.StartSyntheticCompactStatePruneTask()

	// Wire task polling adaptor factory (breaks service -> relay import cycle)
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor {
		a := relay.GetTaskAdaptor(platform)
		if a == nil {
			return nil
		}
		return a
	}

	// Channel upstream model update check task
	controller.StartChannelUpstreamModelUpdateTask()

	if common.IsMasterNode && constant.UpdateTask {
		gopool.Go(func() {
			controller.UpdateMidjourneyTaskBulk()
		})
		gopool.Go(func() {
			controller.UpdateTaskBulk()
		})
	}
	if os.Getenv("BATCH_UPDATE_ENABLED") == "true" {
		common.BatchUpdateEnabled = true
		common.SysLog("batch update enabled with interval " + strconv.Itoa(common.BatchUpdateInterval) + "s")
		model.InitBatchUpdater()
	}

	if os.Getenv("ENABLE_PPROF") == "true" {
		gopool.Go(func() {
			log.Println(http.ListenAndServe("0.0.0.0:8005", nil))
		})
		go common.Monitor()
		common.SysLog("pprof enabled")
	}

	err = common.StartPyroScope()
	if err != nil {
		common.SysError(fmt.Sprintf("start pyroscope error : %v", err))
	}

	// Initialize HTTP server
	server := gin.New()
	// gin trusts every proxy by default, so any caller can set X-Forwarded-For
	// and choose the client IP that the login/registration/email rate limiters
	// and the per-token IP allowlist compare against. Only trust the forwarding
	// headers when the deployment names its proxies, or asks this process to
	// work them out from its own environment with TRUSTED_PROXIES=auto:
	// loopback always, plus the container subnet inside a container.
	trustedProxies, err := common.ResolveTrustedProxies(common.GetEnvOrDefaultString("TRUSTED_PROXIES", ""))
	if err != nil {
		common.FatalLog("invalid TRUSTED_PROXIES: " + err.Error())
	}
	if err := server.SetTrustedProxies(trustedProxies); err != nil {
		common.FatalLog("invalid TRUSTED_PROXIES: " + err.Error())
	}
	if len(trustedProxies) == 0 {
		common.SysLog("trusted proxies disabled; forwarding headers are ignored")
	} else {
		common.SysLog("trusted proxies: " + strings.Join(trustedProxies, ","))
	}
	server.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		common.SysLog(fmt.Sprintf("panic detected: %v", err))
		// Report only a request id: the panic value can carry internal state
		// (paths, upstream addresses) that must not reach the client.
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Panic detected. Please submit a issue here: https://github.com/Calcium-Ion/new-api (request_id: %s)", c.GetString(common.RequestIdKey)),
				"type":    "new_api_panic",
			},
		})
	}))
	// This will cause SSE not to work!!!
	//server.Use(gzip.Gzip(gzip.DefaultCompression))
	server.Use(middleware.RequestId())
	server.Use(middleware.PoweredBy())
	server.Use(middleware.I18n())
	// Registered before router.SetRouter, which is what makes the headers cover
	// every route and both NoRoute fallbacks; sitting early in the chain also
	// means a response aborted by a later middleware still carries them.
	server.Use(middleware.SecurityHeaders())
	middleware.SetUpLogger(server)
	// Initialize session store
	store := cookie.NewStore([]byte(common.SessionSecret))
	store.Options(middleware.SessionCookieOptions(false))
	server.Use(sessions.Sessions("session", store))
	// Mark the session cookie Secure on TLS requests (direct or via a proxy's
	// X-Forwarded-Proto) without breaking plain-HTTP deployments.
	server.Use(middleware.SecureSessionCookie())

	InjectUmamiAnalytics()
	InjectGoogleAnalytics()

	// 设置路由
	router.SetRouter(server, router.ThemeAssets{
		DefaultBuildFS:   buildFS,
		DefaultIndexPage: indexPage,
		ClassicBuildFS:   classicBuildFS,
		ClassicIndexPage: classicIndexPage,
	})
	var port = os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(*common.Port)
	}

	// ReadHeaderTimeout bounds how long a client may take to send its request
	// headers, which is what a slowloris connection exploits. ReadTimeout and
	// WriteTimeout are deliberately NOT set: streaming relay responses are
	// long-lived, and a WriteTimeout would sever them mid-stream. IdleTimeout
	// only applies between requests on a keep-alive connection, so it is safe
	// for SSE.
	srv := &http.Server{
		Addr:              common.ResolveListenAddress(port),
		Handler:           server,
		ReadHeaderTimeout: time.Duration(common.GetEnvOrDefault("READ_HEADER_TIMEOUT_SECONDS", 30)) * time.Second,
		IdleTimeout:       time.Duration(common.GetEnvOrDefault("IDLE_TIMEOUT_SECONDS", 120)) * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			common.FatalLog("failed to start HTTP server: " + err.Error())
		}
	}()

	common.LogStartupSuccess(startTime, port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	common.SysLog(fmt.Sprintf("received signal: %v, shutting down...", sig))

	shutdownTimeout := time.Duration(common.GetEnvOrDefault("SHUTDOWN_TIMEOUT_SECONDS", 120)) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		common.SysError(fmt.Sprintf("server forced to shutdown: %v", err))
	}
	// Stop the periodic batch writer and wait for any in-flight database write
	// before flushing the final in-memory deltas and closing the pool.
	model.StopBatchUpdater()
	// Quota charged during the last batch interval is still in memory at this
	// point. Flushing after Shutdown drains the request path first, so no new
	// deltas are produced while this write runs. A SIGKILL skips this entirely.
	model.FlushBatchUpdates()
	// 看板聚合缓存（CacheQuotaData）只在 DataExportInterval（默认 5 分钟）的
	// 定时器里落盘；不在这里补一次，重启会丢掉最近一个窗口的聚合。放在
	// FlushBatchUpdates 之后：请求路径已排空，不会再产生新聚合。
	if common.DataExportEnabled {
		model.SaveQuotaDataCache()
	}
	common.SysLog("server exited")
}

func InjectUmamiAnalytics() {
	analyticsInjectBuilder := &strings.Builder{}
	if os.Getenv("UMAMI_WEBSITE_ID") != "" {
		umamiSiteID := os.Getenv("UMAMI_WEBSITE_ID")
		umamiScriptURL := os.Getenv("UMAMI_SCRIPT_URL")
		if umamiScriptURL == "" {
			umamiScriptURL = "https://analytics.umami.is/script.js"
		}
		analyticsInjectBuilder.WriteString("<script defer src=\"")
		analyticsInjectBuilder.WriteString(umamiScriptURL)
		analyticsInjectBuilder.WriteString("\" data-website-id=\"")
		analyticsInjectBuilder.WriteString(umamiSiteID)
		analyticsInjectBuilder.WriteString("\"></script>")
	}
	analyticsInjectBuilder.WriteString("<!--Umami QuantumNous-->\n")
	analyticsInject := []byte(analyticsInjectBuilder.String())
	placeholder := []byte("<!--umami-->\n")
	indexPage = bytes.ReplaceAll(indexPage, placeholder, analyticsInject)
	classicIndexPage = bytes.ReplaceAll(classicIndexPage, placeholder, analyticsInject)
}

func InjectGoogleAnalytics() {
	analyticsInjectBuilder := &strings.Builder{}
	if os.Getenv("GOOGLE_ANALYTICS_ID") != "" {
		gaID := os.Getenv("GOOGLE_ANALYTICS_ID")
		// Google Analytics 4 (gtag.js)
		analyticsInjectBuilder.WriteString("<script async src=\"https://www.googletagmanager.com/gtag/js?id=")
		analyticsInjectBuilder.WriteString(gaID)
		analyticsInjectBuilder.WriteString("\"></script>")
		analyticsInjectBuilder.WriteString("<script>")
		analyticsInjectBuilder.WriteString("window.dataLayer = window.dataLayer || [];")
		analyticsInjectBuilder.WriteString("function gtag(){dataLayer.push(arguments);}")
		analyticsInjectBuilder.WriteString("gtag('js', new Date());")
		analyticsInjectBuilder.WriteString("gtag('config', '")
		analyticsInjectBuilder.WriteString(gaID)
		analyticsInjectBuilder.WriteString("');")
		analyticsInjectBuilder.WriteString("</script>")
	}
	analyticsInjectBuilder.WriteString("<!--Google Analytics QuantumNous-->\n")
	analyticsInject := []byte(analyticsInjectBuilder.String())
	placeholder := []byte("<!--Google Analytics-->\n")
	indexPage = bytes.ReplaceAll(indexPage, placeholder, analyticsInject)
	classicIndexPage = bytes.ReplaceAll(classicIndexPage, placeholder, analyticsInject)
}

func InitResources() error {
	// Initialize resources here if needed
	// This is a placeholder function for future resource initialization
	var err error
	dotenvMissing := false
	if !strings.EqualFold(os.Getenv("NEW_API_SKIP_DOTENV"), "true") {
		err = godotenv.Load(".env")
		if err != nil {
			dotenvMissing = true
		}
	}

	// 加载环境变量
	common.InitEnv()
	if dotenvMissing && common.DebugEnabled {
		common.SysLog("No .env file found, using default environment variables. If needed, please create a .env file and set the relevant variables.")
	}

	logger.SetupLogger()

	// Initialize model settings
	ratio_setting.InitRatioSettings()

	service.InitHttpClient()

	service.InitTokenEncoders()

	// Initialize SQL Database
	err = model.InitDB()
	if err != nil {
		common.FatalLog("failed to initialize database: " + err.Error())
		return err
	}

	model.CheckSetup()

	// Initialize options, should after model.InitDB()
	model.InitOptionMap()

	// 清理旧的磁盘缓存文件
	common.CleanupOldCacheFiles()

	// 初始化模型
	model.GetPricing()

	// Initialize SQL Database
	err = model.InitLogDB()
	if err != nil {
		return err
	}
	controller.StartLogRetentionTask()

	// Initialize Redis
	err = common.InitRedisClient()
	if err != nil {
		return err
	}

	perfmetrics.Init()

	// 启动系统监控
	common.StartSystemMonitor()

	// Initialize i18n
	err = i18n.Init()
	if err != nil {
		common.SysError("failed to initialize i18n: " + err.Error())
		// Don't return error, i18n is not critical
	} else {
		common.SysLog("i18n initialized with languages: " + strings.Join(i18n.SupportedLanguages(), ", "))
	}
	// Register user language loader for lazy loading
	i18n.SetUserLangLoader(model.GetUserLanguage)

	// Load custom OAuth providers from database
	err = oauth.LoadCustomProviders()
	if err != nil {
		common.SysError("failed to load custom OAuth providers: " + err.Error())
		// Don't return error, custom OAuth is not critical
	}

	return nil
}
