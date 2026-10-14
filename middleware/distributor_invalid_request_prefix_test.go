package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// getModelFromRequest used to wrap its own parse errors with
// MsgDistributorInvalidRequest, and Distribute wraps the result again, so the
// client saw "Invalid request: Invalid request: EOF". The prefix belongs to the
// single caller that turns the error into a response.
func TestGetModelFromRequestReturnsUnprefixedParseError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	cases := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "truncated json", body: `{"model":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body))
			ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

			_, err := getModelFromRequest(ctx)
			require.Error(t, err)
			require.NotContains(t, err.Error(), i18n.T(ctx, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": ""}))
		})
	}
}
