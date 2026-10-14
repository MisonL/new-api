package relay

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupTaskFetchDB gives the fetch builders a real SQLite database so the test
// exercises the stored-row path rather than a hand-built DTO.
func setupTaskFetchDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	service.InitHttpClient()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
}

// A task row whose stored `data` column is not valid JSON makes the fetch
// response unmarshallable. The builder must surface that as an error; returning
// (nil, nil) makes RelayTaskFetch report a generic "empty response" 502 and
// hides the real cause.
func TestSunoFetchByIDReportsMarshalFailureForInvalidStoredJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTaskFetchDB(t)

	task := &model.Task{
		TaskID:   "suno-marshal-1",
		UserId:   7,
		Platform: constant.TaskPlatformSuno,
		Status:   model.TaskStatusSuccess,
		Data:     []byte("not json"),
	}
	require.NoError(t, model.DB.Create(task).Error)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/task/suno-marshal-1", nil)
	c.Set("id", 7)
	c.Params = gin.Params{{Key: "id", Value: task.TaskID}}

	respBody, taskErr := sunoFetchByIDRespBodyBuilder(c)
	require.Nil(t, respBody, "invalid stored JSON must not produce a response body")
	require.NotNil(t, taskErr, "builder must report the marshal failure instead of returning (nil, nil)")
	require.Equal(t, "marshal_response_failed", taskErr.Code)
}

// The list endpoint (POST /suno/fetch) resolves tasks through GetByTaskIds and
// marshals them the same way, so it needs the same guarantee: a stored row whose
// `data` column is not valid JSON must surface as marshal_response_failed rather
// than (nil, nil).
func TestSunoFetchListReportsMarshalFailureForInvalidStoredJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupTaskFetchDB(t)

	task := &model.Task{
		TaskID:   "suno-marshal-2",
		UserId:   7,
		Platform: constant.TaskPlatformSuno,
		Status:   model.TaskStatusSuccess,
		Data:     []byte("not json"),
	}
	require.NoError(t, model.DB.Create(task).Error)

	body := fmt.Sprintf(`{"ids":["%s"],"action":"fetch"}`, task.TaskID)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/suno/fetch", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 7)

	respBody, taskErr := sunoFetchRespBodyBuilder(c)
	require.Nil(t, respBody, "invalid stored JSON must not produce a response body")
	require.NotNil(t, taskErr, "builder must report the marshal failure instead of returning (nil, nil)")
	require.Equal(t, "marshal_response_failed", taskErr.Code)
}
