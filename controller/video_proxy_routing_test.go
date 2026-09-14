package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type videoProxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn videoProxyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type videoProxyTimeoutError struct{}

func (videoProxyTimeoutError) Error() string   { return "video proxy timeout" }
func (videoProxyTimeoutError) Timeout() bool   { return true }
func (videoProxyTimeoutError) Temporary() bool { return true }

func TestTaskHasDirectVideoResultURL(t *testing.T) {
	setVideoProxyTestServerAddress(t, "https://gateway.example")
	tests := []struct {
		name string
		task *model.Task
		want bool
	}{
		{
			name: "signed URL",
			task: &model.Task{TaskID: "task_signed", PrivateData: model.TaskPrivateData{ResultURL: "https://cdn.example/video.mp4?sig=opaque"}},
			want: true,
		},
		{
			name: "local proxy URL",
			task: &model.Task{TaskID: "task_proxy", PrivateData: model.TaskPrivateData{ResultURL: "https://gateway.example/v1/videos/task_proxy/content"}},
			want: false,
		},
		{
			name: "data URL",
			task: &model.Task{TaskID: "task_data", PrivateData: model.TaskPrivateData{ResultURL: "data:video/mp4;base64,AAAA"}},
			want: false,
		},
		{
			name: "empty URL",
			task: &model.Task{TaskID: "task_empty"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, taskHasDirectVideoResultURL(tt.task))
		})
	}
}

func TestIsTaskProxyContentURLMatchesExactPathOnly(t *testing.T) {
	setVideoProxyTestServerAddress(t, "https://gateway.example")
	for _, tt := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "exact path", url: "https://gateway.example/v1/videos/task_1/content", want: true},
		{name: "query allowed", url: "https://gateway.example/v1/videos/task_1/content?download=1", want: true},
		{name: "fragment allowed", url: "https://gateway.example/v1/videos/task_1/content#download", want: true},
		{name: "path substring is not proxy", url: "https://cdn.example/file?next=/v1/videos/task_1/content", want: false},
		{name: "same path on another host is direct", url: "https://cdn.example/v1/videos/task_1/content", want: false},
		{name: "different scheme is direct", url: "http://gateway.example/v1/videos/task_1/content", want: false},
		{name: "different port is direct", url: "https://gateway.example:8443/v1/videos/task_1/content", want: false},
		{name: "different task", url: "https://gateway.example/v1/videos/task_2/content", want: false},
		{name: "trailing path is not proxy", url: "https://gateway.example/v1/videos/task_1/content/extra", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isTaskProxyContentURL(tt.url, "task_1"))
		})
	}
}

func TestIsTaskProxyContentURLHonorsServerAddressPathPrefix(t *testing.T) {
	setVideoProxyTestServerAddress(t, "https://gateway.example/edge/api/")

	require.True(t, isTaskProxyContentURL("https://gateway.example/edge/api/v1/videos/task_1/content?download=1", "task_1"))
	require.False(t, isTaskProxyContentURL("https://gateway.example/v1/videos/task_1/content", "task_1"))
}

func TestIsTaskProxyContentURLFailsClosedWhenServerAddressIsInvalid(t *testing.T) {
	setVideoProxyTestServerAddress(t, "")

	require.True(t, isTaskProxyContentURL("https://gateway.example/v1/videos/task_1/content", "task_1"))
	require.False(t, isTaskProxyContentURL("https://gateway.example/v1/videos/task_2/content", "task_1"))
}

func setVideoProxyTestServerAddress(t *testing.T, address string) {
	t.Helper()
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = address
	t.Cleanup(func() { system_setting.ServerAddress = previous })
}

func TestParseVideoDataURLStrictVariants(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantMime  string
		wantBytes string
		wantError string
	}{
		{name: "uppercase scheme and flag", value: "DATA:video/mp4;BASE64,dmlkZW8=", wantMime: "video/mp4", wantBytes: "video"},
		{name: "parameters before and after flag", value: "data:video/mp4;BASE64;charset=utf-8,dmlkZW8=", wantMime: "video/mp4", wantBytes: "video"},
		{name: "default MIME", value: "data:;base64,dmlkZW8=", wantMime: "video/mp4", wantBytes: "video"},
		{name: "invalid MIME", value: "data:not-a-mime;base64,dmlkZW8=", wantError: "MIME"},
		{name: "non-video MIME", value: "data:text/plain;base64,dmlkZW8=", wantError: "unsupported video MIME"},
		{name: "invalid base64", value: "data:video/mp4;base64,%%%", wantError: "base64"},
		{name: "CRLF in payload", value: "data:video/mp4;base64,dmlk\nZW8=", wantError: "control"},
		{name: "duplicate flag", value: "data:video/mp4;base64;BASE64,dmlkZW8=", wantError: "duplicate"},
		{name: "missing payload", value: "data:video/mp4;base64,", wantError: "invalid data url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mimeType, payload, err := parseVideoDataURL(tt.value)
			if tt.wantError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMime, mimeType)
			require.Equal(t, tt.wantBytes, string(payload))
		})
	}
}

func TestVideoProxyCanUseStoredResultByChannelType(t *testing.T) {
	setVideoProxyTestServerAddress(t, "https://gateway.example")
	task := &model.Task{
		TaskID:      "task_result",
		PrivateData: model.TaskPrivateData{ResultURL: "https://cdn.example/video.mp4"},
	}

	require.True(t, videoProxyCanUseStoredResult(constant.ChannelTypeVertexAi, task))
	require.True(t, videoProxyCanUseStoredResult(constant.ChannelTypeDoubaoVideo, task))
	require.False(t, videoProxyCanUseStoredResult(constant.ChannelTypeGemini, task))
	require.False(t, videoProxyCanUseStoredResult(constant.ChannelTypeOpenAI, task))
	require.False(t, videoProxyCanUseStoredResult(constant.ChannelTypeSora, task))

	task.PrivateData.ResultURL = "https://gateway.example/v1/videos/task_result/content"
	require.False(t, videoProxyCanUseStoredResult(constant.ChannelTypeVertexAi, task))
}

func TestShouldUseStoredVideoResultWhenProviderChannelIsUnavailable(t *testing.T) {
	setVideoProxyTestServerAddress(t, "https://gateway.example")
	task := &model.Task{
		TaskID: "task_provider_result",
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
			RouteSnapshot: &model.TaskRouteSnapshot{
				SchemaVersion: model.TaskRouteSnapshotSchemaVersion,
				ChannelType:   constant.ChannelTypeGemini,
			},
		},
	}

	require.True(t, shouldUseStoredVideoResult(task, nil, constant.ChannelTypeGemini))
	require.True(t, shouldUseStoredVideoResult(task, &model.Channel{Status: common.ChannelStatusManuallyDisabled}, constant.ChannelTypeGemini))
	require.False(t, shouldUseStoredVideoResult(task, &model.Channel{Status: common.ChannelStatusEnabled}, constant.ChannelTypeGemini))

	task.PrivateData.ResultURL = "https://gateway.example/v1/videos/task_provider_result/content"
	require.False(t, shouldUseStoredVideoResult(task, nil, constant.ChannelTypeGemini))
}

func TestShouldUseStoredVideoResultWhenChannelTypeWasRepurposed(t *testing.T) {
	setVideoProxyTestServerAddress(t, "https://gateway.example")
	task := &model.Task{
		TaskID: "task_repurposed_channel",
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
			RouteSnapshot: &model.TaskRouteSnapshot{
				SchemaVersion: model.TaskRouteSnapshotSchemaVersion,
				ChannelType:   constant.ChannelTypeGemini,
			},
		},
	}
	channel := &model.Channel{Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI}

	require.True(t, taskRouteSnapshotChannelTypeChanged(task, channel))
	require.True(t, shouldUseStoredVideoResult(task, channel, constant.ChannelTypeGemini))
}

func TestTaskRouteChannelTypeUsesSnapshotWhenChannelUnavailable(t *testing.T) {
	task := &model.Task{
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: &model.TaskRouteSnapshot{
				SchemaVersion: model.TaskRouteSnapshotSchemaVersion,
				ChannelType:   constant.ChannelTypeSora,
			},
		},
	}

	require.Equal(t, constant.ChannelTypeSora, taskRouteChannelType(task.PrivateData.RouteSnapshot, nil))
	require.False(t, videoProxyCanUseStoredResult(
		taskRouteChannelType(task.PrivateData.RouteSnapshot, nil),
		&model.Task{PrivateData: model.TaskPrivateData{ResultURL: "https://cdn.example/video.mp4"}},
	))
}

func TestVideoProxyFetchesDirectResultAfterCredentialRotation(t *testing.T) {
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousFetchSetting := *system_setting.GetFetchSetting()
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	system_setting.GetFetchSetting().EnableSSRFProtection = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	model.DB = db
	model.LOG_DB = db
	service.InitHttpClient()
	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		*system_setting.GetFetchSetting() = previousFetchSetting
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("video-data"))
	}))
	defer upstream.Close()

	channel := &model.Channel{
		Id:     902,
		Type:   constant.ChannelTypeVertexAi,
		Key:    "rotated-key",
		Name:   "direct-result",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, db.Create(channel).Error)

	task := &model.Task{
		TaskID:    "task_direct_result",
		UserId:    8,
		ChannelId: channel.Id,
		Group:     "default",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		PrivateData: model.TaskPrivateData{
			Key:       "old-key-no-longer-configured",
			ResultURL: upstream.URL + "/video.mp4",
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	c.Set("id", task.UserId)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)

	VideoProxy(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "video-data", recorder.Body.String())
}

func TestVideoProxyFetchesDirectResultAfterChannelDeletion(t *testing.T) {
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousFetchSetting := *system_setting.GetFetchSetting()
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	system_setting.GetFetchSetting().EnableSSRFProtection = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	model.DB = db
	model.LOG_DB = db
	service.InitHttpClient()
	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		*system_setting.GetFetchSetting() = previousFetchSetting
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("video-data"))
	}))
	defer upstream.Close()

	channel := &model.Channel{
		Id:     903,
		Type:   constant.ChannelTypeDoubaoVideo,
		Key:    "deleted-channel-key",
		Name:   "deleted-channel",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, db.Create(channel).Error)
	task := &model.Task{
		TaskID:    "task_deleted_channel",
		UserId:    9,
		ChannelId: channel.Id,
		Group:     "default",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		PrivateData: model.TaskPrivateData{
			ResultURL: upstream.URL + "/video.mp4",
		},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	c.Set("id", task.UserId)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)

	VideoProxy(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "video-data", recorder.Body.String())
}

func TestVideoProxyServesStoredDataURLAfterChannelDeletion(t *testing.T) {
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousFetchSetting := *system_setting.GetFetchSetting()
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	system_setting.GetFetchSetting().EnableSSRFProtection = true

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	model.DB = db
	model.LOG_DB = db
	service.InitHttpClient()
	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		*system_setting.GetFetchSetting() = previousFetchSetting
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	task := &model.Task{
		TaskID:    "task_data_result",
		UserId:    10,
		ChannelId: 904,
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		PrivateData: model.TaskPrivateData{
			ResultURL: "data:video/mp4;base64,dmlkZW8tZGF0YQ==",
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	c.Set("id", task.UserId)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)

	VideoProxy(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "video-data", recorder.Body.String())
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
}

func TestResolveVideoProxyAllowsLegacySnapshotWithoutChannel(t *testing.T) {
	for _, schemaVersion := range []int{1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("schema-%d", schemaVersion), func(t *testing.T) {
			proxy, err := resolveVideoProxy(&model.TaskRouteSnapshot{SchemaVersion: schemaVersion}, nil)
			require.NoError(t, err)
			require.Empty(t, proxy)
		})
	}
}

func TestResolveVideoProxyRejectsCorruptCurrentSnapshotWithoutChannel(t *testing.T) {
	proxy, err := resolveVideoProxy(&model.TaskRouteSnapshot{
		SchemaVersion:    model.TaskRouteSnapshotSchemaVersion,
		ProxyCaptured:    true,
		ProxyFingerprint: "corrupt",
	}, nil)
	require.Error(t, err)
	require.Empty(t, proxy)
	require.Contains(t, err.Error(), "required identity fields are incomplete")
}

func TestWriteVideoDataURLRejectsInvalidPayloadBeforeHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	err := writeVideoDataURL(c, "data:video/mp4;base64,%%%")
	require.Error(t, err)
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.Bytes())
}

func TestWriteVideoDataURLRejectsOversizeBeforeHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	payload := base64.StdEncoding.EncodeToString([]byte("1234"))

	err := writeVideoDataURLWithLimit(c, "data:video/mp4;base64,"+payload, 3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds 3 decoded bytes")
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.Bytes())
}

func TestWriteVideoDataURLWritesValidPayloadWithContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	payload := base64.StdEncoding.EncodeToString([]byte("video"))

	require.NoError(t, writeVideoDataURLWithLimit(c, "data:video/mp4;base64,"+payload, 5))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, "video", recorder.Body.String())
}

func TestVideoProxyErrorDoesNotWriteAfterResponseCommitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	c.Writer.Header().Set("Content-Type", "video/mp4")
	c.Writer.WriteHeader(http.StatusOK)
	_, err := c.Writer.Write([]byte("partial-video"))
	require.NoError(t, err)

	videoProxyError(c, http.StatusBadGateway, "server_error", "should not be appended")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, "partial-video", recorder.Body.String())
}

func TestVideoProxyUpstreamStatusPreservesTransientSemantics(t *testing.T) {
	for _, tt := range []struct {
		upstream int
		want     int
	}{
		{http.StatusTooManyRequests, http.StatusTooManyRequests},
		{http.StatusServiceUnavailable, http.StatusServiceUnavailable},
		{http.StatusGatewayTimeout, http.StatusGatewayTimeout},
		{524, http.StatusGatewayTimeout},
		{http.StatusBadRequest, http.StatusBadGateway},
		{http.StatusInternalServerError, http.StatusBadGateway},
	} {
		require.Equal(t, tt.want, videoProxyUpstreamStatus(tt.upstream))
	}
}

func TestVideoProxyTaskRouteErrorPreservesUpstreamStatusAndRetryAfter(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	err := types.NewErrorWithStatusCode(errors.New("upstream timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable)
	err.RetryAfterSeconds = 17

	require.Equal(t, http.StatusServiceUnavailable, videoProxyTaskRouteErrorStatus(c, err))
	require.Equal(t, "17", recorder.Header().Get("Retry-After"))

	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	err.StatusCode = 524
	require.Equal(t, http.StatusGatewayTimeout, videoProxyTaskRouteErrorStatus(c, err))
}

func TestVideoProxyTransportErrorStatusPreservesTimeoutSemantics(t *testing.T) {
	require.Equal(t, http.StatusGatewayTimeout, videoProxyTransportErrorStatus(context.DeadlineExceeded))
	require.Equal(t, http.StatusGatewayTimeout, videoProxyTransportErrorStatus(fmt.Errorf("wrapped: %w", videoProxyTimeoutError{})))
	require.Equal(t, http.StatusBadGateway, videoProxyTransportErrorStatus(errors.New("connection reset")))
}

func TestDoVideoProxyRequestRetriesTransientGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	attempts := 0
	client := &http.Client{Transport: videoProxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts < 3 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("busy")), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video")), Request: req}, nil
	})}
	req := httptest.NewRequest(http.MethodGet, "https://provider.example/video", nil)
	req.RequestURI = ""
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := doVideoProxyRequest(ctx, client, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 3, attempts)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestDoVideoProxyRequestDoesNotRetryPermanentStatus(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: videoProxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("missing")), Request: req}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "https://provider.example/video", nil)
	req.RequestURI = ""

	resp, err := doVideoProxyRequest(ctx, client, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, 1, attempts)
	_ = resp.Body.Close()
}

func TestCopyVideoProxyRetryAfterAllowsValidTransientHeaderOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{Header: http.Header{"Retry-After": []string{"30"}}}

	copyVideoProxyRetryAfter(c, resp, http.StatusServiceUnavailable)
	require.Equal(t, "30", recorder.Header().Get("Retry-After"))

	for _, value := range []string{"-1", "not-a-date", "30\nX-Leak"} {
		recorder = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(recorder)
		resp = &http.Response{Header: http.Header{"Retry-After": []string{value}}}
		copyVideoProxyRetryAfter(c, resp, http.StatusServiceUnavailable)
		require.Empty(t, recorder.Header().Get("Retry-After"), value)
	}
}

func TestApplyVideoProxyRuntimeHeadersPreservesMultipleValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://origin.example/video.mp4", nil)
	headers := http.Header{}
	headers.Add("X-Trace", "first")
	headers.Add("X-Trace", "second")
	headers.Set("User-Agent", "video-proxy-test")

	applyVideoProxyRuntimeHeaders(req, headers)

	require.Equal(t, []string{"first", "second"}, req.Header.Values("X-Trace"))
	require.Equal(t, "video-proxy-test", req.Header.Get("User-Agent"))
}
