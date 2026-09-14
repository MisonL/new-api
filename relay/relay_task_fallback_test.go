package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoFetchFallsBackToPersistedTaskWhenRealtimeFetchFails(t *testing.T) {
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	service.InitHttpClient()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"temporary upstream outage","type":"server_error"}}`))
	}))
	defer upstream.Close()

	channel := &model.Channel{
		Id:      901,
		Type:    constant.ChannelTypeGemini,
		Key:     "realtime-fallback-key",
		Name:    "realtime-fallback",
		Group:   "default",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &upstream.URL,
	}
	require.NoError(t, db.Create(channel).Error)

	taskID := "task_realtime_fallback"
	task := &model.Task{
		TaskID:    taskID,
		Platform:  constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeGemini)),
		UserId:    7,
		ChannelId: channel.Id,
		Group:     "default",
		Action:    constant.TaskActionTextGenerate,
		Status:    model.TaskStatusInProgress,
		Progress:  "50%",
		Data:      json.RawMessage(`{"model":"veo-3.0-generate-001"}`),
		Properties: model.Properties{
			OriginModelName:   "veo-3.0-generate-001",
			UpstreamModelName: "veo-3.0-generate-001",
		},
		PrivateData: model.TaskPrivateData{
			Key:            "realtime-fallback-key",
			UpstreamTaskID: taskcommon.EncodeLocalTaskID("operations/fallback"),
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "task_id", Value: taskID}}
	c.Set("id", 7)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+taskID, nil)

	body, taskErr := videoFetchByIDRespBodyBuilder(c)
	require.Nil(t, taskErr)
	var response dto.TaskResponse[any]
	require.NoError(t, common.Unmarshal(body, &response))
	require.Equal(t, "success", response.Code)
	data, ok := response.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, taskID, data["task_id"])
	require.Equal(t, "IN_PROGRESS", data["status"])
}

func TestRealtimeTaskStatusNormalizationAndProgressBaseline(t *testing.T) {
	status := model.TaskStatus("success")
	normalized := model.TaskStatus(strings.ToUpper(strings.TrimSpace(string(status))))
	require.True(t, isRecognizedRealtimeTaskStatus(normalized))
	require.Equal(t, taskcommon.ProgressComplete, realtimeTaskStatusProgress(normalized))
	require.False(t, isRecognizedRealtimeTaskStatus(model.TaskStatus("UNKNOWN")))
}

func TestApplyRealtimeTaskDataPreservesPayloadWithoutRecognizedStatus(t *testing.T) {
	oldData := json.RawMessage(`{"model":"veo-3.0-generate-001","result":"persisted"}`)
	task := &model.Task{Data: oldData}

	applyRealtimeTaskData(task, []byte(`{"unexpected":"sparse-2xx"}`), true, false)

	require.Equal(t, oldData, task.Data)
}

func TestApplyRealtimeTaskDataUpdatesPayloadForAcceptedRecognizedStatus(t *testing.T) {
	task := &model.Task{Data: json.RawMessage(`{"result":"old"}`)}
	body := []byte(`{"response":{"done":true}}`)

	applyRealtimeTaskData(task, body, true, true)

	require.JSONEq(t, string(body), string(task.Data))
}

func TestTryRealtimeFetchSkipsBusyTaskClaimBeforeUpstreamRequest(t *testing.T) {
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = true
	redisServer := miniredis.RunT(t)
	common.RDB = redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	service.InitHttpClient()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		_ = common.RDB.Close()
		common.RDB = previousRDB
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	var requestCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"temporary upstream outage","type":"server_error"}}`))
	}))
	defer upstream.Close()

	channel := &model.Channel{
		Id:      902,
		Type:    constant.ChannelTypeGemini,
		Key:     "realtime-claim-key",
		Name:    "realtime-claim",
		Group:   "default",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &upstream.URL,
	}
	require.NoError(t, db.Create(channel).Error)
	task := &model.Task{
		TaskID:    "task_realtime_claim",
		Platform:  constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeGemini)),
		UserId:    8,
		ChannelId: channel.Id,
		Group:     "default",
		Action:    constant.TaskActionTextGenerate,
		Status:    model.TaskStatusInProgress,
		Progress:  "50%",
		Data:      json.RawMessage(`{"model":"veo-3.0-generate-001"}`),
		Properties: model.Properties{
			OriginModelName:   "veo-3.0-generate-001",
			UpstreamModelName: "veo-3.0-generate-001",
		},
		PrivateData: model.TaskPrivateData{
			Key:            "realtime-claim-key",
			UpstreamTaskID: taskcommon.EncodeLocalTaskID("operations/claim"),
		},
	}
	require.NoError(t, db.Create(task).Error)

	claim, err := service.AcquireTaskPollingClaim(context.Background(), task)
	require.NoError(t, err)
	require.NotNil(t, claim)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", task.UserId)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID, nil)

	response, err := tryRealtimeFetch(c, task, false)
	require.Nil(t, response)
	require.ErrorIs(t, err, service.ErrTaskPollingClaimBusy)
	require.Equal(t, int32(0), requestCount.Load())

	claim.Release()
	response, err = tryRealtimeFetch(c, task, false)
	require.Nil(t, response)
	require.Error(t, err)
	require.Equal(t, int32(1), requestCount.Load())
}
