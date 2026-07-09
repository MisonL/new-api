package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type npmVersionRouteResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Data    struct {
		PackageName   string                        `json:"package"`
		Source        string                        `json:"source"`
		LatestVersion string                        `json:"latest_version"`
		Options       []service.NpmCLIVersionOption `json:"options"`
	} `json:"data"`
}

func TestChannelNpmVersionOptionRoutesUseAdminAuthAndCriticalRateLimit(t *testing.T) {
	engine, user, commonUser := setupNpmVersionAPIRouterTest(t)
	cookies := loginNpmVersionAPIRouterTestUser(t, engine, user.Id)

	firstGet := performNpmVersionAPIRouterRequest(
		engine,
		http.MethodGet,
		"/api/channel/npm_version_options?package=@openai/codex",
		user.Id,
		cookies,
		"198.51.100.201:1000",
	)
	require.Equal(t, http.StatusOK, firstGet.Code)
	getBody := decodeNpmVersionRouteResponse(t, firstGet)
	require.True(t, getBody.Success)
	require.Equal(t, "@openai/codex", getBody.Data.PackageName)
	require.Equal(t, "recorded", getBody.Data.Source)
	require.Equal(t, "1.2.3", getBody.Data.LatestVersion)

	secondGet := performNpmVersionAPIRouterRequest(
		engine,
		http.MethodGet,
		"/api/channel/npm_version_options?package=@openai/codex",
		user.Id,
		cookies,
		"198.51.100.201:1001",
	)
	require.Equal(t, http.StatusTooManyRequests, secondGet.Code)

	firstPost := performNpmVersionAPIRouterRequest(
		engine,
		http.MethodPost,
		"/api/channel/npm_version_options/refresh?package=@scope/unknown",
		user.Id,
		cookies,
		"198.51.100.202:1000",
	)
	require.Equal(t, http.StatusOK, firstPost.Code)
	postBody := decodeNpmVersionRouteResponse(t, firstPost)
	require.False(t, postBody.Success)
	require.Equal(t, service.NpmCLIPackageUnsupportedCode, postBody.Code)
	require.Equal(t, "@scope/unknown", postBody.Data.PackageName)

	unauthenticatedDiagnostics := performNpmVersionAPIRouterRequest(
		engine,
		http.MethodGet,
		"/api/channel/npm_version_options/diagnostics",
		0,
		nil,
		"198.51.100.204:1000",
	)
	require.Equal(t, http.StatusUnauthorized, unauthenticatedDiagnostics.Code)

	commonCookies := loginNpmVersionAPIRouterTestUser(t, engine, commonUser.Id)
	commonDiagnostics := performNpmVersionAPIRouterRequest(
		engine,
		http.MethodGet,
		"/api/channel/npm_version_options/diagnostics",
		commonUser.Id,
		commonCookies,
		"198.51.100.205:1000",
	)
	require.Equal(t, http.StatusOK, commonDiagnostics.Code)
	commonDiagnosticsBody := decodeNpmVersionRouteResponse(t, commonDiagnostics)
	require.False(t, commonDiagnosticsBody.Success)

	firstDiagnostics := performNpmVersionAPIRouterRequest(
		engine,
		http.MethodGet,
		"/api/channel/npm_version_options/diagnostics",
		user.Id,
		cookies,
		"198.51.100.203:1000",
	)
	require.Equal(t, http.StatusOK, firstDiagnostics.Code)
	require.Contains(t, firstDiagnostics.Body.String(), `"packages"`)

	secondDiagnostics := performNpmVersionAPIRouterRequest(
		engine,
		http.MethodGet,
		"/api/channel/npm_version_options/diagnostics",
		user.Id,
		cookies,
		"198.51.100.203:1001",
	)
	require.Equal(t, http.StatusTooManyRequests, secondDiagnostics.Code)

	secondPost := performNpmVersionAPIRouterRequest(
		engine,
		http.MethodPost,
		"/api/channel/npm_version_options/refresh?package=@scope/unknown",
		user.Id,
		cookies,
		"198.51.100.202:1001",
	)
	require.Equal(t, http.StatusTooManyRequests, secondPost.Code)
}

func setupNpmVersionAPIRouterTest(t *testing.T) (*gin.Engine, *model.User, *model.User) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousGlobalAPIRateLimitEnable := common.GlobalApiRateLimitEnable
	previousCriticalRateLimitEnable := common.CriticalRateLimitEnable
	previousCriticalRateLimitNum := common.CriticalRateLimitNum
	previousCriticalRateLimitDuration := common.CriticalRateLimitDuration
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousOptionMap := common.OptionMap

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.GlobalApiRateLimitEnable = false
	common.CriticalRateLimitEnable = true
	common.CriticalRateLimitNum = 1
	common.CriticalRateLimitDuration = 60 * 60
	common.OptionMap = map[string]string{}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}))

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		common.GlobalApiRateLimitEnable = previousGlobalAPIRateLimitEnable
		common.CriticalRateLimitEnable = previousCriticalRateLimitEnable
		common.CriticalRateLimitNum = previousCriticalRateLimitNum
		common.CriticalRateLimitDuration = previousCriticalRateLimitDuration
		common.OptionMap = previousOptionMap
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	password, err := common.Password2Hash("12345678")
	require.NoError(t, err)
	user := &model.User{
		Username:    "npm-admin",
		Password:    password,
		DisplayName: "npm-admin",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "npm-admin-aff",
	}
	require.NoError(t, model.DB.Create(user).Error)
	commonUser := &model.User{
		Username:    "npm-common",
		Password:    password,
		DisplayName: "npm-common",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "npm-common-aff",
	}
	require.NoError(t, model.DB.Create(commonUser).Error)

	recorded := map[string]any{
		"packages": map[string]any{
			"@openai/codex": map[string]any{
				"fetched_at":     time.Unix(1000, 0).UTC(),
				"latest_version": "1.2.3",
				"options": []service.NpmCLIVersionOption{
					{Value: "latest", Label: "latest (1.2.3)", IsLatest: true, ResolvedVersion: "1.2.3"},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)
	common.OptionMap[npmCliVersionRecordedOptionsKeyForRouterTest()] = string(payload)

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("npm-version-api-router-test-secret"))))
	engine.GET("/test/login/:id", func(c *gin.Context) {
		userID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		user, err := model.GetUserById(userID, false)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
			return
		}
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	SetApiRouter(engine)

	return engine, user, commonUser
}

func npmCliVersionRecordedOptionsKeyForRouterTest() string {
	return "NpmCLIVersionRecordedOptions"
}

func loginNpmVersionAPIRouterTestUser(t *testing.T, engine *gin.Engine, userID int) []*http.Cookie {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/test/login/%d", userID), nil)
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	return recorder.Result().Cookies()
}

func performNpmVersionAPIRouterRequest(engine *gin.Engine, method string, target string, userID int, cookies []*http.Cookie, remoteAddr string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	request.RemoteAddr = remoteAddr
	if userID > 0 {
		request.Header.Set("New-Api-User", strconv.Itoa(userID))
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeNpmVersionRouteResponse(t *testing.T, recorder *httptest.ResponseRecorder) npmVersionRouteResponse {
	t.Helper()

	var body npmVersionRouteResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}
