package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetTopUpInfoIncludesTopUpLink(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousTopUpLink := common.TopUpLink
	common.TopUpLink = "https://pay.example.test/topup"
	t.Cleanup(func() {
		common.TopUpLink = previousTopUpLink
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup/info", nil)

	GetTopUpInfo(c)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			TopUpLink string `json:"topup_link"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, "https://pay.example.test/topup", response.Data.TopUpLink)
}
