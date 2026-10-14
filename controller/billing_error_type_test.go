package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// GetUsage answers with HTTP 200 and an error body, matching the OpenAI billing
// API shape. It was the last producer still writing the internal
// "new_api_error" marker into the field clients switch on.
func TestGetUsageUsesStandardErrorTypeOnFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousDisplayTokenStat := common.DisplayTokenStatEnabled
	previousDB := model.DB
	// A database with no `users` table makes the quota read fail deterministically
	// without a live dependency; the point of the test is the error body, not the
	// query.
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	common.DisplayTokenStatEnabled = false
	model.DB = db
	t.Cleanup(func() {
		common.DisplayTokenStatEnabled = previousDisplayTokenStat
		model.DB = previousDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/dashboard/billing/usage", nil)
	ctx.Set("id", 4242)

	GetUsage(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"error"`)
	require.Contains(t, recorder.Body.String(), `"type":"server_error"`)
	require.NotContains(t, recorder.Body.String(), "new_api_error")
}
